// Package inboxsection is the Inbox view: one merged list of notifications
// and searches, with pins and numbered slots.
package inboxsection

import (
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/inbox"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/table"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

const (
	SectionType = "inbox"

	statePollInterval = 2 * time.Second
	defaultLimit      = 50
)

// RowData adapts an inbox item to the row interface the rest of the UI uses.
type RowData struct {
	inbox.Item
}

func (r *RowData) GetRepoNameWithOwner() string { return r.Repo }
func (r *RowData) GetTitle() string             { return r.Title }
func (r *RowData) GetNumber() int               { return r.Number }
func (r *RowData) GetUrl() string               { return r.URL }
func (r *RowData) GetUpdatedAt() time.Time      { return r.UpdatedAt }

type Model struct {
	section.BaseModel
	Source Source
	Store  inbox.Store
	Host   string
	// Items is the arranged list; its first rows are the slots.
	Items []inbox.Item

	merged       []inbox.Item
	fetched      bool
	lastSlots    []inbox.Slot
	slotsWritten bool
	stateTimes   [2]time.Time
	polling      bool
}

// FetchedMsg carries the merged items of a finished fetch.
type FetchedMsg struct {
	TaskId string
	Items  []inbox.Item
	Err    error
}

type stateTickMsg struct{}

func NewModel(id int, ctx *context.ProgramContext, src Source, store inbox.Store, host string) Model {
	m := Model{Source: src, Store: store, Host: host}
	m.BaseModel = section.NewModel(
		ctx,
		section.NewSectionOptions{
			Id:          id,
			Config:      config.SectionConfig{Title: "Inbox"},
			Type:        SectionType,
			Columns:     columns(),
			Singular:    m.GetItemSingularForm(),
			Plural:      m.GetItemPluralForm(),
			LastUpdated: time.Now(),
			CreatedAt:   time.Now(),
		},
	)
	m.Table.SetContentHeight(2)
	m.Table.EmptyState = utils.StringPtr(
		ctx.Styles.Section.EmptyStateStyle.Render("Nothing needs you right now"),
	)
	return m
}

func columns() []table.Column {
	slotWidth, iconWidth, groupWidth, updatedWidth := 3, 4, 22, 10
	return []table.Column{
		{Title: "#", Width: &slotWidth},
		{Title: "", Width: &iconWidth},
		{Title: "Title", Grow: utils.BoolPtr(true)},
		{Title: "Why", Width: &groupWidth},
		{Title: "Updated", Width: &updatedWidth},
	}
}

func (m *Model) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	if wrapped, ok := msg.(section.SectionMsg); ok {
		msg = wrapped.InternalMsg
	}

	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keys.NotificationKeys.ToggleBookmark):
			m.togglePin()
		case key.Matches(msg, keys.NotificationKeys.MarkAsDone):
			cmd = m.markDone()
		}

	case FetchedMsg:
		if msg.TaskId == m.LastFetch.TaskId {
			m.SetIsLoading(false)
			m.PageInfo = &data.PageInfo{HasNextPage: false}
			if msg.Err == nil {
				m.merged = msg.Items
				m.fetched = true
				m.SetLastUpdated(time.Now())
				m.rebuild()
			}
		}

	case stateTickMsg:
		if m.fetched && m.Store.ModTimes() != m.stateTimes {
			m.rebuild()
		}
		cmd = m.pollState()
	}

	tbl, tableCmd := m.Table.Update(msg)
	m.Table = tbl

	return m, tea.Batch(cmd, tableCmd)
}

// rebuild re-reads pins and dismissals from disk (the slot command may have
// changed them), re-arranges the list and exports the slots when they changed.
func (m *Model) rebuild() {
	pins, err := m.Store.LoadPins()
	if err != nil {
		log.Error("failed loading pins", "err", err)
	}
	if refreshed, changed := inbox.RefreshPins(pins, m.merged); changed {
		pins = refreshed
		if err := m.Store.SavePins(pins); err != nil {
			log.Error("failed saving pins", "err", err)
		}
	}
	dismissed, err := m.Store.LoadDismissed()
	if err != nil {
		log.Error("failed loading dismissed items", "err", err)
	}

	m.Items = inbox.Arrange(m.merged, pins, dismissed, m.groupOrder())
	m.TotalCount = len(m.Items)
	m.Table.SetRows(m.BuildRows())
	m.UpdateTotalItemsCount(m.TotalCount)
	for m.CurrRow() > 0 && m.CurrRow() >= len(m.Items) {
		m.PrevRow()
	}

	slots := inbox.SlotsOf(m.Items)
	if !m.slotsWritten || !inbox.SlotsEqual(slots, m.lastSlots) {
		if err := m.Store.WriteSlots(m.Host, slots, time.Now()); err != nil {
			log.Error("failed writing slots", "err", err)
		} else {
			m.lastSlots = slots
			m.slotsWritten = true
		}
	}
	m.stateTimes = m.Store.ModTimes()
}

func (m *Model) groupOrder() []inbox.Group {
	return inbox.NormalizeGroupOrder(m.Ctx.Config.Inbox.Groups)
}

func (m *Model) currItem() (inbox.Item, bool) {
	idx := m.Table.GetCurrItem()
	if idx < 0 || idx >= len(m.Items) {
		return inbox.Item{}, false
	}
	return m.Items[idx], true
}

func (m *Model) togglePin() {
	item, ok := m.currItem()
	if !ok {
		return
	}
	if _, err := m.Store.TogglePin(item); err != nil {
		log.Error("failed toggling pin", "err", err)
		return
	}
	m.rebuild()
}

// markDone hides the current item locally and, when it is backed by a
// notification, marks that thread done on GitHub.
func (m *Model) markDone() tea.Cmd {
	item, ok := m.currItem()
	if !ok {
		return nil
	}
	if err := m.Store.Done(item); err != nil {
		log.Error("failed dismissing item", "err", err)
		return nil
	}
	m.rebuild()
	if !item.HasNotification() {
		return nil
	}

	taskId := fmt.Sprintf("inbox_done_%s_%d", item.ThreadID, time.Now().UnixNano())
	startCmd := m.Ctx.StartTask(context.Task{
		Id:           taskId,
		StartText:    "Marking notification as done",
		FinishedText: "Notification marked as done",
		State:        context.TaskStart,
	})
	src := m.Source
	return tea.Batch(startCmd, func() tea.Msg {
		return constants.TaskFinishedMsg{
			TaskId:      taskId,
			SectionId:   m.Id,
			SectionType: SectionType,
			Err:         src.MarkDone(item.ThreadID),
		}
	})
}

func (m *Model) pollState() tea.Cmd {
	return tea.Tick(statePollInterval, func(time.Time) tea.Msg {
		return section.SectionMsg{Id: m.Id, Type: SectionType, InternalMsg: stateTickMsg{}}
	})
}

func (m *Model) limit() int {
	if limit := m.Ctx.Config.Inbox.Limit; limit > 0 {
		return limit
	}
	return defaultLimit
}

func (m *Model) FetchNextPageSectionRows() []tea.Cmd {
	if m == nil {
		return nil
	}
	if m.PageInfo != nil && !m.PageInfo.HasNextPage {
		return nil
	}

	taskId := fmt.Sprintf("fetching_inbox_%d_%d", m.Id, time.Now().UnixNano())
	m.LastFetch.TaskId = taskId
	m.LastFetch.At = time.Now()
	startCmd := m.Ctx.StartTask(context.Task{
		Id:           taskId,
		StartText:    "Fetching inbox",
		FinishedText: "Inbox has been fetched",
		State:        context.TaskStart,
	})

	src, store, limit := m.Source, m.Store, m.limit()
	fetchCmd := func() tea.Msg {
		pins, _ := store.LoadPins()
		items, err := Collect(src, limit, pins)
		return constants.TaskFinishedMsg{
			SectionId:   m.Id,
			SectionType: SectionType,
			TaskId:      taskId,
			Err:         err,
			Msg:         FetchedMsg{TaskId: taskId, Items: items, Err: err},
		}
	}

	cmds := []tea.Cmd{startCmd, fetchCmd, m.SetIsLoading(true)}
	if !m.polling {
		m.polling = true
		cmds = append(cmds, m.pollState())
	}
	return cmds
}

func (m *Model) BuildRows() []table.Row {
	rows := make([]table.Row, 0, len(m.Items))
	for i, item := range m.Items {
		rows = append(rows, m.renderRow(i, item))
	}
	return rows
}

func (m *Model) NumRows() int {
	return len(m.Items)
}

func (m *Model) GetCurrRow() data.RowData {
	item, ok := m.currItem()
	if !ok {
		return nil
	}
	return &RowData{Item: item}
}

func (m *Model) ResetRows() {
	// Keep the current list (and with it the slots) while a refresh runs.
	m.ResetPageInfo()
}

// The inbox has no search: filtering would move rows and with them the slots.
func (m *Model) SetIsSearching(bool) tea.Cmd {
	return nil
}

func (m *Model) View() string {
	return m.Ctx.Styles.Section.ContainerStyle.
		Width(m.Ctx.MainContentWidth).
		Render(lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), m.Table.View()))
}

func (m *Model) viewHeader() string {
	pinned := 0
	for _, item := range m.Items {
		if item.Pinned {
			pinned++
		}
	}
	faint := lipgloss.NewStyle().Foreground(m.Ctx.Theme.FaintText)
	parts := []string{
		lipgloss.NewStyle().Bold(true).Foreground(m.Ctx.Theme.PrimaryText).Render("Inbox"),
		faint.Render(m.Host),
		faint.Render(fmt.Sprintf("%d items, %d pinned", len(m.Items), pinned)),
		faint.Render(fmt.Sprintf(
			"%s pin, %s done",
			keys.NotificationKeys.ToggleBookmark.Help().Key,
			keys.NotificationKeys.MarkAsDone.Help().Key,
		)),
	}
	width := m.GetDimensions().Width
	text := ansi.Truncate(strings.Join(parts, faint.Render("  ·  ")), max(width-4, 0), "…")
	return m.Ctx.Styles.Search.Root.Width(width).Render(text)
}

func (m *Model) GetItemSingularForm() string {
	return "Item"
}

func (m *Model) GetItemPluralForm() string {
	return "Items"
}

func (m *Model) GetTotalCount() int {
	return m.TotalCount
}

func (m *Model) GetPagerContent() string {
	pagerContent := ""
	if m.TotalCount > 0 {
		pagerContent = fmt.Sprintf(
			"%v %v • %v %v/%v",
			constants.WaitingIcon,
			m.LastUpdated().Format("01/02 15:04:05"),
			m.SingularForm,
			m.Table.GetCurrItem()+1,
			m.TotalCount,
		)
	}
	return m.Ctx.Styles.ListViewPort.PagerStyle.Render(pagerContent)
}

func InitSections(ctx *context.ProgramContext) []section.Section {
	host := ctx.Config.ResolveHost()
	store := inbox.Store{Dir: inbox.DefaultStateDir(host)}
	if err := os.MkdirAll(store.Dir, 0o755); err != nil {
		log.Error("failed creating inbox state dir", "err", err)
	}
	return []section.Section{new(NewModel(0, ctx, GitHubSource{}, store, host))}
}

// FetchAllSections returns a batch command refreshing the inbox.
func FetchAllSections(sections []section.Section) tea.Cmd {
	cmds := make([]tea.Cmd, 0)
	for _, s := range section.ToImplSections[*Model](sections) {
		s.PageInfo = nil
		cmds = append(cmds, s.FetchNextPageSectionRows()...)
	}
	return tea.Batch(cmds...)
}
