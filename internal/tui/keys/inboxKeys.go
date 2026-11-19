package keys

import "charm.land/bubbles/v2/key"

// InboxFullHelp lists the keys of the Inbox view. Pin and done reuse the
// notification bindings so a rebind of toggleBookmark or markAsDone applies to
// both views.
func InboxFullHelp() []key.Binding {
	pin := NotificationKeys.ToggleBookmark
	pin.SetHelp(pin.Help().Key, "toggle pin")
	details := NotificationKeys.View
	details.SetHelp(details.Help().Key, "view details")
	switchView := PRKeys.ViewIssues
	switchView.SetHelp(switchView.Help().Key, "switch view")
	return []key.Binding{
		details,
		pin,
		NotificationKeys.MarkAsDone,
		switchView,
	}
}
