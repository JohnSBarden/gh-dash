package inboxsection

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/inbox"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

const testHost = "github.example.com"

var t0 = time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

type fakeSource struct {
	mu            sync.Mutex
	notifications []data.NotificationData
	searches      map[string][]data.SearchIssue
	subjects      map[string]data.SearchIssue
	done          []string
	err           error
}

func (f *fakeSource) Notifications(int) ([]data.NotificationData, error) {
	return f.notifications, f.err
}

func (f *fakeSource) Search(query string, _ int) ([]data.SearchIssue, error) {
	return f.searches[query], f.err
}

func (f *fakeSource) Subject(repo string, number int) (data.SearchIssue, error) {
	issue, ok := f.subjects[fmt.Sprintf("%s#%d", repo, number)]
	if !ok {
		return data.SearchIssue{}, errors.New("not found")
	}
	return issue, nil
}

func (f *fakeSource) MarkDone(threadID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done = append(f.done, threadID)
	return nil
}

func pull(n int, hours int) data.SearchIssue {
	issue := data.SearchIssue{
		HtmlUrl:   fmt.Sprintf("https://%s/acme/widgets/pull/%d", testHost, n),
		Title:     fmt.Sprintf("Pull %d", n),
		Number:    n,
		State:     "open",
		UpdatedAt: t0.Add(time.Duration(hours) * time.Hour),
	}
	issue.PullRequest = &struct {
		MergedAt *time.Time `json:"merged_at"`
	}{}
	return issue
}

func notification(id string, n int, reason string, hours int) data.NotificationData {
	nd := data.NotificationData{
		Id:        id,
		Unread:    true,
		Reason:    reason,
		UpdatedAt: t0.Add(time.Duration(hours) * time.Hour),
	}
	nd.Subject.Title = fmt.Sprintf("Pull %d", n)
	nd.Subject.Type = inbox.TypePullRequest
	nd.Subject.Url = fmt.Sprintf("https://%s/api/v3/repos/acme/widgets/pulls/%d", testHost, n)
	nd.Repository.FullName = "acme/widgets"
	nd.Repository.HtmlUrl = fmt.Sprintf("https://%s/acme/widgets", testHost)
	return nd
}

func url(n int) string {
	return fmt.Sprintf("https://%s/acme/widgets/pull/%d", testHost, n)
}

func newSource() *fakeSource {
	return &fakeSource{
		notifications: []data.NotificationData{
			notification("t5", 5, "mention", 9),
			notification("t1", 1, "review_requested", 8),
		},
		searches: map[string][]data.SearchIssue{
			QueryReviewRequested: {pull(1, 1), pull(2, 2)},
			QueryMine:            {pull(3, 3)},
			QueryReviewed:        {pull(4, 4), pull(2, 2)},
		},
		subjects: map[string]data.SearchIssue{},
	}
}

func newTestModel(t *testing.T, src Source) *Model {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("confirmQuit: false\n"), 0o644))
	cfg, err := config.ParseConfig(config.Location{ConfigFlag: cfgPath, SkipGlobalConfig: true})
	require.NoError(t, err)

	ctx := &context.ProgramContext{
		Config:            &cfg,
		Theme:             *theme.DefaultTheme,
		Styles:            context.DefaultStyles,
		ScreenWidth:       120,
		ScreenHeight:      40,
		MainContentWidth:  120,
		MainContentHeight: 30,
		StartTask: func(context.Task) tea.Cmd {
			return func() tea.Msg { return nil }
		},
	}
	m := NewModel(0, ctx, src, inbox.Store{Dir: filepath.Join(t.TempDir(), "state")}, testHost)
	m.polling = true // keep the poll timer out of the tests
	return &m
}

// fetch runs the section's fetch command and feeds the result back in.
func fetch(t *testing.T, m *Model) {
	t.Helper()
	m.PageInfo = nil
	for _, cmd := range m.FetchNextPageSectionRows() {
		if cmd == nil {
			continue
		}
		if finished, ok := cmd().(constants.TaskFinishedMsg); ok {
			require.Equal(t, SectionType, finished.SectionType)
			m.Update(finished.Msg)
			return
		}
	}
	t.Fatal("no fetch command returned")
}

func itemURLs(m *Model) []string {
	urls := make([]string, len(m.Items))
	for i, item := range m.Items {
		urls[i] = item.URL
	}
	return urls
}

func press(m *Model, r rune) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	return cmd
}

func TestFetchMergesGroupsAndExportsSlots(t *testing.T) {
	m := newTestModel(t, newSource())
	fetch(t, m)

	// review requested (newest first), mine, reviewed, other notifications
	require.Equal(t, []string{url(1), url(2), url(3), url(4), url(5)}, itemURLs(m))
	require.Equal(t, []string{"review_requested", "reviewed"}, m.Items[1].Reasons)
	require.Equal(t, "t1", m.Items[0].ThreadID)
	require.Equal(t, inbox.GroupNotifications, m.Items[4].Group)
	require.False(t, m.GetIsLoading())

	doc, err := m.Store.ReadSlots()
	require.NoError(t, err)
	require.Equal(t, testHost, doc.Host)
	require.False(t, doc.GeneratedAt.IsZero())
	require.Len(t, doc.Slots, 5)
	third, err := doc.Find(3)
	require.NoError(t, err)
	require.Equal(t, "Pull 3", third.Title)
	require.Equal(t, "acme/widgets", third.Repo)

	rows := m.BuildRows()
	require.Len(t, rows, 5)
	require.Contains(t, rows[0][0], "1")
	require.Contains(t, rows[4][0], "5")
	require.Contains(t, m.View(), "Inbox")
}

func TestSlotNumbersIgnoreTheCursor(t *testing.T) {
	m := newTestModel(t, newSource())
	fetch(t, m)
	before, err := os.ReadFile(m.Store.SlotsPath())
	require.NoError(t, err)

	m.NextRow()
	m.NextRow()
	m.Update(stateTickMsg{})

	after, err := os.ReadFile(m.Store.SlotsPath())
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
	require.Equal(t, "3", SlotLabel(2))
	require.Equal(t, "", SlotLabel(9))
}

func TestPinMovesItemToTheTopAndPersists(t *testing.T) {
	m := newTestModel(t, newSource())
	fetch(t, m)

	m.NextRow()
	m.NextRow()
	press(m, 'b')

	require.Equal(t, []string{url(3), url(1), url(2), url(4), url(5)}, itemURLs(m))
	require.True(t, m.Items[0].Pinned)
	pins, err := m.Store.LoadPins()
	require.NoError(t, err)
	require.Len(t, pins, 1)
	require.Equal(t, url(3), pins[0].Key)
	doc, err := m.Store.ReadSlots()
	require.NoError(t, err)
	require.Equal(t, url(3), doc.Slots[0].URL)

	m.FirstItem()
	press(m, 'b')
	require.Equal(t, []string{url(1), url(2), url(3), url(4), url(5)}, itemURLs(m))
}

func TestPinnedItemStaysAfterItLeavesTheSources(t *testing.T) {
	src := newSource()
	m := newTestModel(t, src)
	fetch(t, m)
	m.NextRow()
	m.NextRow()
	press(m, 'b') // pin pull 3

	merged := pull(3, 20)
	merged.State = "closed"
	now := t0
	merged.PullRequest.MergedAt = &now
	src.searches[QueryMine] = nil
	src.subjects["acme/widgets#3"] = merged
	fetch(t, m)

	require.Equal(t, url(3), m.Items[0].URL)
	require.True(t, m.Items[0].Pinned)
	require.Equal(t, inbox.StateMerged, m.Items[0].State)
	require.Len(t, m.Items, 5)
}

func TestDoneOnSearchOnlyItemDismissesUntilUpdated(t *testing.T) {
	src := newSource()
	m := newTestModel(t, src)
	fetch(t, m)

	m.NextRow() // pull 2 has no notification
	cmd := press(m, 'D')

	require.Nil(t, cmd)
	require.Equal(t, []string{url(1), url(3), url(4), url(5)}, itemURLs(m))
	require.Empty(t, src.done)

	fetch(t, m)
	require.NotContains(t, itemURLs(m), url(2), "stays hidden while unchanged")

	src.searches[QueryReviewRequested] = []data.SearchIssue{pull(1, 1), pull(2, 30)}
	fetch(t, m)
	require.Contains(t, itemURLs(m), url(2), "reappears once updated_at changes")
}

func TestDoneOnNotificationAlsoMarksTheThreadDone(t *testing.T) {
	src := newSource()
	m := newTestModel(t, src)
	fetch(t, m)

	cmd := press(m, 'D') // pull 1: search result and notification
	require.NotNil(t, cmd)
	require.NotContains(t, itemURLs(m), url(1))

	batch, ok := cmd().(tea.BatchMsg)
	require.True(t, ok)
	for _, c := range batch {
		if c != nil {
			c()
		}
	}
	require.Equal(t, []string{"t1"}, src.done)

	dismissed, err := m.Store.LoadDismissed()
	require.NoError(t, err)
	require.Contains(t, dismissed, url(1))
}

func TestChangesFromAnotherProcessAreNoticed(t *testing.T) {
	m := newTestModel(t, newSource())
	fetch(t, m)

	// what `gh dash slot 1 --done` does
	require.NoError(t, m.Store.Done(m.Items[0]))
	m.Update(stateTickMsg{})

	require.Equal(t, []string{url(2), url(3), url(4), url(5)}, itemURLs(m))
	doc, err := m.Store.ReadSlots()
	require.NoError(t, err)
	require.Equal(t, url(2), doc.Slots[0].URL)
}

func TestFetchErrorKeepsTheList(t *testing.T) {
	src := newSource()
	m := newTestModel(t, src)
	fetch(t, m)

	src.err = errors.New("boom")
	fetch(t, m)

	require.Len(t, m.Items, 5)
	require.False(t, m.GetIsLoading())
}

func TestSearchIsDisabled(t *testing.T) {
	m := newTestModel(t, newSource())
	require.Nil(t, m.SetIsSearching(true))
	require.False(t, m.IsSearchFocused())
}

func TestRenderPreview(t *testing.T) {
	m := newTestModel(t, newSource())
	fetch(t, m)
	out := RenderPreview(m.Ctx, m.Items[0], 0, 60)
	require.Contains(t, out, "Pull 1")
	require.Contains(t, out, "acme/widgets #1")
	require.Contains(t, out, "Review requested")
}
