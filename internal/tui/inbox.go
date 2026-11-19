package tui

import (
	tea "charm.land/bubbletea/v2"
	log "charm.land/log/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/inbox"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/inboxsection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
)

// inboxSubject is the pull request or issue loaded for the inbox row with the
// given key, shown read-only in the preview.
type inboxSubject struct {
	key   string
	pr    *prrow.Data
	issue *data.IssueData
}

type inboxSubjectFetchedMsg struct {
	key   string
	pr    *data.EnrichedPullRequestData
	issue *data.IssueData
	err   error
}

// fetchInboxInBackground keeps the inbox, and with it slots.json, current
// while another view is active. Slots never depend on the active view.
func (m *Model) fetchInboxInBackground() tea.Cmd {
	if m.ctx.View == config.InboxView {
		return nil
	}
	return inboxsection.FetchAllSections(m.inbox)
}

// loadInboxSubject fetches the details of the current inbox row. Subjects
// that are neither pull requests nor issues are opened in the browser.
func (m *Model) loadInboxSubject() tea.Cmd {
	row, ok := m.getCurrRowData().(*inboxsection.RowData)
	if !ok || row == nil {
		return nil
	}
	key, url := row.Key, row.URL
	switch row.Type {
	case inbox.TypePullRequest:
		return func() tea.Msg {
			pr, err := data.FetchPullRequest(url)
			return inboxSubjectFetchedMsg{key: key, pr: &pr, err: err}
		}
	case inbox.TypeIssue:
		return func() tea.Msg {
			issue, err := data.FetchIssue(url)
			return inboxSubjectFetchedMsg{key: key, issue: &issue, err: err}
		}
	default:
		return m.openBrowser()
	}
}

func (m *Model) onInboxSubjectFetched(msg inboxSubjectFetchedMsg) tea.Cmd {
	if msg.err != nil {
		log.Error("failed fetching inbox subject", "err", msg.err)
		return m.notifyErr("Failed loading details: " + msg.err.Error())
	}
	m.inboxSubject = inboxSubject{key: msg.key, issue: msg.issue}
	if msg.pr != nil {
		prData := msg.pr.ToPullRequestData()
		m.inboxSubject.pr = &prrow.Data{Primary: &prData, Enriched: *msg.pr, IsEnriched: true}
		m.prView.SetEnrichedPR(*msg.pr)
		m.prView.GoToFirstTab()
	}
	m.sidebar.ScrollToTop()
	return m.syncSidebar()
}

func (m *Model) syncInboxSidebar(row *inboxsection.RowData, width int) {
	subject := m.inboxSubject
	switch {
	case subject.key == row.Key && subject.pr != nil:
		m.prView.SetSectionId(0)
		m.prView.SetRow(subject.pr)
		m.prView.SetWidth(width)
		m.sidebar.SetContent(m.prView.View())
	case subject.key == row.Key && subject.issue != nil:
		m.issueSidebar.SetSectionId(0)
		m.issueSidebar.SetRow(subject.issue)
		m.issueSidebar.SetWidth(width)
		m.sidebar.SetContent(m.issueSidebar.View())
	default:
		slot := -1
		if s := m.getCurrSection(); s != nil {
			slot = s.CurrRow()
		}
		m.sidebar.SetContent(inboxsection.RenderPreview(m.ctx, row.Item, slot, width))
	}
}
