package inboxsection

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/inbox"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/table"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

const (
	pinIcon        = "" // nf-fa-bookmark
	issueIcon      = "" // nf-oct-issue_opened
	issueDoneIcon  = "" // nf-oct-issue_closed
	nonBreakingGap = " "
)

// Cells use raw style prefixes without resets so the selected row background
// is kept across the whole row.
func prefix(style lipgloss.Style) string {
	return utils.GetStylePrefix(style)
}

// SlotLabel is the gutter text of a row: 1-9 for the slots, blank after.
func SlotLabel(index int) string {
	if index < 0 || index >= inbox.MaxSlots {
		return ""
	}
	return fmt.Sprint(index + 1)
}

func (m *Model) renderRow(index int, item inbox.Item) table.Row {
	theme := m.Ctx.Theme
	faint := prefix(lipgloss.NewStyle().Foreground(theme.FaintText))
	secondary := prefix(lipgloss.NewStyle().Foreground(theme.SecondaryText))

	slot := prefix(lipgloss.NewStyle().Bold(true).Foreground(theme.WarningText)) +
		SlotLabel(index) + "\n"

	marker := ""
	switch {
	case item.Pinned:
		marker = prefix(lipgloss.NewStyle().Foreground(theme.WarningText)) + pinIcon
	case item.Unread:
		marker = prefix(lipgloss.NewStyle().Foreground(lipgloss.Color("33"))) + constants.DotIcon
	}
	icon := stateIcon(m.Ctx, item) + "\n" + marker

	ref := item.Repo
	if item.Number > 0 {
		ref = fmt.Sprintf("%s #%d", item.Repo, item.Number)
	}
	titleStyle := lipgloss.NewStyle().Foreground(theme.PrimaryText)
	if item.Unread {
		titleStyle = titleStyle.Bold(true)
	}
	title := secondary + ref + "\n" + prefix(titleStyle) + item.Title

	why := prefix(lipgloss.NewStyle().Foreground(theme.PrimaryText)) + item.Group.Title() +
		"\n" + faint + strings.Join(item.Reasons, ", ")

	updated := faint + utils.TimeElapsed(item.UpdatedAt) + nonBreakingGap + "ago" + "\n"

	return table.Row{slot, icon, title, why, updated}
}

func stateIcon(ctx *context.ProgramContext, item inbox.Item) string {
	colors := ctx.Styles.Colors
	style := lipgloss.NewStyle()
	icon := constants.NotificationIcon
	switch item.Type {
	case inbox.TypePullRequest:
		switch {
		case item.State == inbox.StateMerged:
			style, icon = style.Foreground(colors.MergedPR), constants.MergedIcon
		case item.State == inbox.StateClosed:
			style, icon = style.Foreground(colors.ClosedPR), constants.ClosedIcon
		case item.Draft:
			style, icon = style.Foreground(ctx.Theme.FaintText), constants.DraftIcon
		default:
			style, icon = style.Foreground(colors.OpenPR), constants.OpenIcon
		}
	case inbox.TypeIssue:
		if item.State == inbox.StateClosed {
			style, icon = style.Foreground(colors.ClosedPR), issueDoneIcon
		} else {
			style, icon = style.Foreground(colors.OpenIssue), issueIcon
		}
	default:
		style = style.Foreground(ctx.Theme.SecondaryText)
	}
	return prefix(style) + icon
}

// RenderPreview is the sidebar content for an inbox row.
func RenderPreview(ctx *context.ProgramContext, item inbox.Item, slot int, width int) string {
	theme := ctx.Theme
	label := lipgloss.NewStyle().Foreground(theme.FaintText).Width(10)
	value := lipgloss.NewStyle().Foreground(theme.PrimaryText)
	line := func(name, text string) string {
		return lipgloss.JoinHorizontal(lipgloss.Top, label.Render(name), value.Render(text))
	}

	ref := item.Repo
	if item.Number > 0 {
		ref = fmt.Sprintf("%s #%d", item.Repo, item.Number)
	}
	state := item.State
	if item.Draft && state == inbox.StateOpen {
		state = "draft"
	}
	slotText := "none (past the first 9 rows)"
	if label := SlotLabel(slot); label != "" {
		slotText = label
	}
	pinned := "no"
	if item.Pinned {
		pinned = "yes"
	}

	lines := []string{
		lipgloss.NewStyle().Bold(true).Foreground(theme.PrimaryText).
			Width(max(width-4, 10)).Render(item.Title),
		lipgloss.NewStyle().Foreground(theme.SecondaryText).Render(ref),
		"",
		line("Slot", slotText),
		line("Group", item.Group.Title()),
		line("Why", strings.Join(item.Reasons, ", ")),
	}
	if state != "" {
		lines = append(lines, line("State", state))
	}
	lines = append(lines,
		line("Pinned", pinned),
		line("Updated", utils.TimeElapsed(item.UpdatedAt)+" ago"),
		line("URL", item.URL),
		"",
		lipgloss.NewStyle().Foreground(theme.FaintText).Render(
			"enter view details  o open in browser  b pin  D done",
		),
	)
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}
