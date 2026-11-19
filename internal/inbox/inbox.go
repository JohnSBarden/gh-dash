// Package inbox holds the pure model behind the Inbox view: merging items from
// several sources, de-duplicating them, grouping, pinning, dismissing and
// numbering the first rows as slots. It has no TUI or network dependencies.
package inbox

import (
	"sort"
	"strings"
	"time"
)

// Subject types, matching the GitHub notifications API.
const (
	TypePullRequest = "PullRequest"
	TypeIssue       = "Issue"
)

// Item states.
const (
	StateOpen   = "open"
	StateClosed = "closed"
	StateMerged = "merged"
)

// Reasons added by the search sources. Notification reasons are kept verbatim
// (review_requested, mention, assign, subscribed, ...).
const (
	ReasonReviewRequested = "review_requested"
	ReasonMine            = "mine"
	ReasonReviewed        = "reviewed"
	// ReasonAuthor is the notification reason for threads the user created.
	ReasonAuthor = "author"
)

// Group is a block of the inbox list.
type Group string

const (
	GroupPinned          Group = "pinned"
	GroupReviewRequested Group = "review_requested"
	GroupMine            Group = "mine"
	GroupReviewed        Group = "reviewed"
	GroupNotifications   Group = "notifications"
)

// DefaultGroupOrder is the order groups are listed in after the pins.
var DefaultGroupOrder = []Group{
	GroupReviewRequested,
	GroupMine,
	GroupReviewed,
	GroupNotifications,
}

// MaxSlots is the number of rows that get a slot number.
const MaxSlots = 9

// Title returns the human readable name of a group.
func (g Group) Title() string {
	switch g {
	case GroupPinned:
		return "Pinned"
	case GroupReviewRequested:
		return "Review requested"
	case GroupMine:
		return "Mine"
	case GroupReviewed:
		return "Reviewed by me"
	default:
		return "Other notifications"
	}
}

// Item is one row of the inbox.
type Item struct {
	// Key identifies the item across sources: the subject's web URL for pull
	// requests and issues, or a thread based key for subjects without one.
	Key       string    `json:"key"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Repo      string    `json:"repo"`
	Number    int       `json:"number,omitempty"`
	Type      string    `json:"type,omitempty"`
	State     string    `json:"state,omitempty"`
	Draft     bool      `json:"draft,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	Reasons   []string  `json:"reasons"`
	// ThreadID is the notification thread, empty for search-only items.
	ThreadID string `json:"thread_id,omitempty"`
	Unread   bool   `json:"unread,omitempty"`
	Pinned   bool   `json:"pinned,omitempty"`
	Group    Group  `json:"group,omitempty"`
}

// HasNotification reports whether the item is backed by a notification thread.
func (i Item) HasNotification() bool {
	return i.ThreadID != ""
}

// Merge de-duplicates items by key. A merged item keeps every reason it
// matched, the newest updated_at and the notification thread if any source
// had one. The result is ordered by first appearance.
func Merge(sources ...[]Item) []Item {
	index := map[string]int{}
	merged := []Item{}
	for _, source := range sources {
		for _, item := range source {
			if item.Key == "" {
				continue
			}
			pos, seen := index[item.Key]
			if !seen {
				item.Reasons = appendUnique(nil, item.Reasons...)
				index[item.Key] = len(merged)
				merged = append(merged, item)
				continue
			}
			merged[pos] = mergeItem(merged[pos], item)
		}
	}
	return merged
}

func mergeItem(a, b Item) Item {
	a.Reasons = appendUnique(a.Reasons, b.Reasons...)
	if b.UpdatedAt.After(a.UpdatedAt) {
		a.UpdatedAt = b.UpdatedAt
	}
	if a.ThreadID == "" {
		a.ThreadID = b.ThreadID
	}
	a.Unread = a.Unread || b.Unread
	a.Draft = a.Draft || b.Draft
	if a.Title == "" {
		a.Title = b.Title
	}
	if a.URL == "" {
		a.URL = b.URL
	}
	if a.Repo == "" {
		a.Repo = b.Repo
	}
	if a.Number == 0 {
		a.Number = b.Number
	}
	if a.Type == "" {
		a.Type = b.Type
	}
	if a.State == "" {
		a.State = b.State
	}
	return a
}

func appendUnique(dst []string, values ...string) []string {
	if dst == nil {
		dst = []string{}
	}
	for _, v := range values {
		if v == "" {
			continue
		}
		found := false
		for _, d := range dst {
			if d == v {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, v)
		}
	}
	return dst
}

func groupOfReason(reason string) Group {
	switch reason {
	case ReasonReviewRequested:
		return GroupReviewRequested
	case ReasonMine, ReasonAuthor:
		return GroupMine
	case ReasonReviewed:
		return GroupReviewed
	default:
		return GroupNotifications
	}
}

// NormalizeGroupOrder parses a configured group order. Unknown names are
// dropped and groups left out are appended in their default order.
func NormalizeGroupOrder(names []string) []Group {
	order := []Group{}
	seen := map[Group]bool{}
	add := func(g Group) {
		if !seen[g] {
			seen[g] = true
			order = append(order, g)
		}
	}
	for _, name := range names {
		g := Group(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "-", "_"))
		switch g {
		case GroupReviewRequested, GroupMine, GroupReviewed, GroupNotifications:
			add(g)
		}
	}
	for _, g := range DefaultGroupOrder {
		add(g)
	}
	return order
}

// GroupOf returns the group of an item: the first group in order that one of
// its reasons maps to.
func GroupOf(item Item, order []Group) Group {
	matched := map[Group]bool{}
	for _, reason := range item.Reasons {
		matched[groupOfReason(reason)] = true
	}
	for _, g := range order {
		if matched[g] {
			return g
		}
	}
	return GroupNotifications
}

// Arrange turns merged items into the final list: pins first in pin order,
// then each group in order with the newest updated_at first. Dismissed items
// are hidden until their updated_at moves past the dismissal, pinned items are
// never hidden and stay listed from their stored snapshot when no source
// returns them any more. Items without a reason (looked up only because they
// were pinned) are not listed once unpinned.
func Arrange(items []Item, pins []Pin, dismissed Dismissed, order []Group) []Item {
	if len(order) == 0 {
		order = DefaultGroupOrder
	}
	byKey := make(map[string]Item, len(items))
	for _, item := range items {
		byKey[item.Key] = item
	}

	result := make([]Item, 0, len(items)+len(pins))
	pinned := map[string]bool{}
	for _, pin := range pins {
		if pin.Key == "" || pinned[pin.Key] {
			continue
		}
		pinned[pin.Key] = true
		item, ok := byKey[pin.Key]
		if !ok {
			item = pin.item()
		}
		item.Pinned = true
		item.Group = GroupPinned
		result = append(result, item)
	}

	rank := make(map[Group]int, len(order))
	for i, g := range order {
		rank[g] = i
	}
	rest := make([]Item, 0, len(items))
	for _, item := range items {
		if pinned[item.Key] || len(item.Reasons) == 0 || dismissed.Hides(item) {
			continue
		}
		item.Pinned = false
		item.Group = GroupOf(item, order)
		rest = append(rest, item)
	}
	sort.SliceStable(rest, func(i, j int) bool {
		a, b := rest[i], rest[j]
		if rank[a.Group] != rank[b.Group] {
			return rank[a.Group] < rank[b.Group]
		}
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
		return a.Key < b.Key
	})
	return append(result, rest...)
}
