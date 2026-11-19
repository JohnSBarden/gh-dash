package slotcmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/handoff"
	"github.com/dlvhdr/gh-dash/v4/internal/inbox"
)

var now = time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

type recorder struct {
	runs   [][]string
	stderr string
	done   []string
	opened []string
}

func (r *recorder) Run(argv []string) (string, string, error) {
	r.runs = append(r.runs, argv)
	if r.stderr != "" {
		return "", r.stderr, errors.New("exit status 1")
	}
	return "", "", nil
}

func (r *recorder) Exec(argv []string, _ string) error {
	r.runs = append(r.runs, argv)
	return nil
}

func setup(t *testing.T, input string) (Deps, *recorder, *bytes.Buffer) {
	t.Helper()
	store := inbox.Store{Dir: t.TempDir()}
	searchOnly := inbox.FromSearch(inbox.SearchInput{
		HTMLURL:       "https://github.example.com/acme/widgets/pull/7",
		Title:         "Add gears",
		Number:        7,
		State:         "open",
		IsPullRequest: true,
		UpdatedAt:     now.Add(-time.Hour),
	}, inbox.ReasonMine)
	withThread := inbox.FromNotification(inbox.NotificationInput{
		ThreadID:    "t42",
		Reason:      "mention",
		UpdatedAt:   now.Add(-2 * time.Hour),
		Title:       "Gears squeak",
		SubjectType: inbox.TypeIssue,
		SubjectURL:  "https://github.example.com/api/v3/repos/acme/widgets/issues/8",
		Repo:        "acme/widgets",
		RepoURL:     "https://github.example.com/acme/widgets",
	})
	require.NoError(t, store.WriteSlots(
		"github.example.com",
		inbox.SlotsOf([]inbox.Item{searchOnly, withThread}),
		now.Add(-time.Minute),
	))

	rec := &recorder{}
	out := &bytes.Buffer{}
	return Deps{
		Store:       store,
		In:          strings.NewReader(input),
		Out:         out,
		Interactive: true,
		Agent: config.AgentConfig{
			Mode: config.AgentModePane,
			Pane: config.AgentPaneConfig{Target: "reviewer"},
		},
		Env:    handoff.Env{Multiplexer: handoff.Herdr},
		Runner: rec,
		MarkDone: func(id string) error {
			rec.done = append(rec.done, id)
			return nil
		},
		Open: func(url string) error {
			rec.opened = append(rec.opened, url)
			return nil
		},
		Now: func() time.Time { return now },
	}, rec, out
}

func TestMissingSlotsFile(t *testing.T) {
	deps, _, _ := setup(t, "")
	deps.Store = inbox.Store{Dir: t.TempDir()}
	err := Run(Options{Slot: 1, Action: ActionOpen}, deps)
	require.ErrorIs(t, err, inbox.ErrNoSlots)
}

func TestEmptySlotIsAnErrorAndNothingRuns(t *testing.T) {
	deps, rec, _ := setup(t, "do it\n")
	err := Run(Options{Slot: 5, Action: ActionAgent}, deps)
	require.ErrorContains(t, err, "slot 5 is empty")
	require.Empty(t, rec.runs)
}

func TestAgentShowsTheItemThenAsksForThePrompt(t *testing.T) {
	deps, rec, out := setup(t, "Review this\n")
	require.NoError(t, Run(Options{Slot: 1, Action: ActionAgent}, deps))

	shown := out.String()
	require.Less(t, strings.Index(shown, "Add gears"), strings.Index(shown, "Prompt"),
		"the title is printed before the prompt is asked for")
	require.Contains(t, shown, "acme/widgets #7")
	require.Equal(t, [][]string{{
		"herdr", "agent", "prompt", "reviewer",
		"Review this\n\nAdd gears — https://github.example.com/acme/widgets/pull/7",
	}}, rec.runs)
}

func TestAgentPromptFlagNeedsNoTerminal(t *testing.T) {
	deps, rec, _ := setup(t, "")
	deps.Interactive = false
	require.NoError(t, Run(Options{Slot: 2, Action: ActionAgent, Prompt: "Triage"}, deps))
	require.Len(t, rec.runs, 1)
	require.Contains(t, rec.runs[0][4], "Gears squeak")

	err := Run(Options{Slot: 2, Action: ActionAgent}, deps)
	require.ErrorContains(t, err, "--prompt")
}

func TestAgentEmptyPromptCancels(t *testing.T) {
	deps, rec, _ := setup(t, "\n")
	require.ErrorIs(t, Run(Options{Slot: 1, Action: ActionAgent}, deps), ErrCancelled)
	require.Empty(t, rec.runs)
}

func TestAgentBlockedIsReportedOnce(t *testing.T) {
	deps, rec, _ := setup(t, "Review this\n")
	rec.stderr = `{"error":{"code":"agent_blocked","message":"blocked"}}`
	require.ErrorIs(t, Run(Options{Slot: 1, Action: ActionAgent}, deps), handoff.ErrAgentBlocked)
	require.Len(t, rec.runs, 1)
}

func TestDoneOnSearchOnlyItemDismissesLocally(t *testing.T) {
	deps, rec, out := setup(t, "\n")
	require.NoError(t, Run(Options{Slot: 1, Action: ActionDone}, deps))

	require.Empty(t, rec.done)
	dismissed, err := deps.Store.LoadDismissed()
	require.NoError(t, err)
	require.Contains(t, dismissed, "https://github.example.com/acme/widgets/pull/7")
	require.Contains(t, out.String(), "Add gears")
}

func TestDoneOnNotificationAlsoMarksTheThread(t *testing.T) {
	deps, rec, _ := setup(t, "")
	require.NoError(t, Run(Options{Slot: 2, Action: ActionDone, Yes: true}, deps))

	require.Equal(t, []string{"t42"}, rec.done)
	dismissed, err := deps.Store.LoadDismissed()
	require.NoError(t, err)
	require.Contains(t, dismissed, "https://github.example.com/acme/widgets/issues/8")
}

func TestDoneCanBeDeclined(t *testing.T) {
	deps, rec, _ := setup(t, "n\n")
	require.ErrorIs(t, Run(Options{Slot: 2, Action: ActionDone}, deps), ErrCancelled)
	require.Empty(t, rec.done)
	dismissed, err := deps.Store.LoadDismissed()
	require.NoError(t, err)
	require.Empty(t, dismissed)
}

func TestOpen(t *testing.T) {
	deps, rec, _ := setup(t, "")
	require.NoError(t, Run(Options{Slot: 1, Action: ActionOpen}, deps))
	require.Equal(t, []string{"https://github.example.com/acme/widgets/pull/7"}, rec.opened)
}

func TestStaleSlotsWarn(t *testing.T) {
	deps, _, out := setup(t, "")
	deps.Now = func() time.Time { return now.Add(5 * time.Hour) }
	require.NoError(t, Run(Options{Slot: 1, Action: ActionOpen}, deps))
	require.Contains(t, out.String(), "Warning")
}
