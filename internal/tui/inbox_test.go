package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/inbox"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/inboxsection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issuessection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/sidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tabs"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newInboxTestModel(t *testing.T, view config.ViewType) (*Model, *inboxsection.Model) {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)

	ctx := &context.ProgramContext{Config: &cfg, View: view}
	ctx.Theme = theme.ParseTheme(ctx.Config)
	ctx.Styles = context.InitStyles(ctx.Theme)
	ctx.StartTask = func(task context.Task) tea.Cmd { return nil }
	ctx.ScreenWidth, ctx.ScreenHeight = 120, 40
	ctx.MainContentWidth, ctx.MainContentHeight = 120, 30

	sidebarModel := sidebar.NewModel()
	sidebarModel.UpdateProgramContext(ctx)
	sidebarModel.IsOpen = true

	m := &Model{
		ctx:     ctx,
		keys:    keys.Keys,
		prView:  prview.NewModel(ctx),
		sidebar: sidebarModel,
		tabs:    tabs.NewModel(ctx),
	}
	prSec := prssection.NewModel(0, ctx, config.PrsSectionConfig{}, time.Now(), time.Now())
	m.prs = []section.Section{&prSec}
	issueSec := issuessection.NewModel(0, ctx, config.IssuesSectionConfig{}, time.Now(), time.Now())
	m.issues = []section.Section{&issueSec}

	inboxSec := inboxsection.NewModel(
		0,
		ctx,
		nil,
		inbox.Store{Dir: t.TempDir()},
		"github.example.com",
	)
	m.inbox = []section.Section{&inboxSec}
	return m, &inboxSec
}

func TestInboxView_IsPartOfTheViewCycle(t *testing.T) {
	m, _ := newInboxTestModel(t, config.InboxView)
	require.Len(t, m.getCurrentViewSections(), 1)

	m.switchSelectedView()
	require.Equal(t, config.NotificationsView, m.ctx.View)
	m.switchSelectedView()
	require.Equal(t, config.PRsView, m.ctx.View)
	m.switchSelectedView()
	require.Equal(t, config.IssuesView, m.ctx.View)
	m.switchSelectedView()
	require.Equal(t, config.InboxView, m.ctx.View)
}

func TestInboxView_UpdatesReachTheInboxFromAnyView(t *testing.T) {
	m, inboxSec := newInboxTestModel(t, config.PRsView)
	item := inbox.FromSearch(inbox.SearchInput{
		HTMLURL:       "https://github.example.com/acme/widgets/pull/7",
		Title:         "Add gears",
		Number:        7,
		State:         "open",
		IsPullRequest: true,
		UpdatedAt:     time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC),
	}, inbox.ReasonMine)

	inboxSec.LastFetch.TaskId = "task"
	m.updateSection(0, inboxsection.SectionType, inboxsection.FetchedMsg{
		TaskId: "task",
		Items:  []inbox.Item{item},
	})

	require.Len(t, inboxSec.Items, 1)
	doc, err := inboxSec.Store.ReadSlots()
	require.NoError(t, err, "slots are exported while another view is active")
	require.Equal(t, "Add gears", doc.Slots[0].Title)
}

func TestInboxView_SidebarShowsTheRowSummary(t *testing.T) {
	m, inboxSec := newInboxTestModel(t, config.InboxView)
	inboxSec.LastFetch.TaskId = "task"
	m.updateSection(0, inboxsection.SectionType, inboxsection.FetchedMsg{
		TaskId: "task",
		Items: []inbox.Item{inbox.FromSearch(inbox.SearchInput{
			HTMLURL: "https://github.example.com/acme/widgets/issues/8",
			Title:   "Gears squeak",
			Number:  8,
			State:   "open",
		}, inbox.ReasonMine)},
	})

	row, ok := m.getCurrRowData().(*inboxsection.RowData)
	require.True(t, ok)
	require.Equal(t, "https://github.example.com/acme/widgets/issues/8", row.GetUrl())
	require.NotPanics(t, func() { m.syncSidebar() })
}
