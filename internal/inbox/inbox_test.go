package inbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2025, 1, 10, 12, 0, 0, 0, time.UTC)

func at(hours int) time.Time {
	return t0.Add(time.Duration(hours) * time.Hour)
}

func pr(n int, updated time.Time, reasons ...string) Item {
	return FromSearch(SearchInput{
		HTMLURL:       "https://github.example.com/acme/widgets/pull/" + itoa(n),
		Title:         "PR " + itoa(n),
		Number:        n,
		State:         "open",
		IsPullRequest: true,
		UpdatedAt:     updated,
	}, "").withReasons(reasons...)
}

func (i Item) withReasons(reasons ...string) Item {
	i.Reasons = append([]string{}, reasons...)
	return i
}

func itoa(n int) string {
	return string(rune('0' + n))
}

func keys(items []Item) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Key
	}
	return out
}

func notif(id string, n int, reason string, updated time.Time) Item {
	return FromNotification(NotificationInput{
		ThreadID:    id,
		Reason:      reason,
		Unread:      true,
		UpdatedAt:   updated,
		Title:       "PR " + itoa(n) + "\r\n",
		SubjectType: TypePullRequest,
		SubjectURL:  "https://github.example.com/api/v3/repos/acme/widgets/pulls/" + itoa(n),
		Repo:        "acme/widgets",
		RepoURL:     "https://github.example.com/acme/widgets",
	})
}

func TestFromNotification(t *testing.T) {
	item := notif("t1", 7, "mention", at(0))
	require.Equal(t, "https://github.example.com/acme/widgets/pull/7", item.Key)
	require.Equal(t, item.Key, item.URL)
	require.Equal(t, 7, item.Number)
	require.Equal(t, "PR 7", item.Title)
	require.True(t, item.HasNotification())

	issue := FromNotification(NotificationInput{
		ThreadID:    "t2",
		Reason:      "assign",
		SubjectType: TypeIssue,
		SubjectURL:  "https://api.github.com/repos/octocat/hello/issues/12",
		Repo:        "octocat/hello",
		RepoURL:     "https://github.com/octocat/hello/",
	})
	require.Equal(t, "https://github.com/octocat/hello/issues/12", issue.Key)

	discussion := FromNotification(NotificationInput{
		ThreadID:    "t3",
		Reason:      "subscribed",
		SubjectType: "Discussion",
		Repo:        "octocat/hello",
		RepoURL:     "https://github.com/octocat/hello",
	})
	require.Equal(t, "thread:t3", discussion.Key)
	require.Equal(t, "https://github.com/octocat/hello", discussion.URL)
}

func TestFromSearch(t *testing.T) {
	item := FromSearch(SearchInput{
		HTMLURL:       "https://github.com/octocat/hello/pull/3",
		Title:         "Add thing",
		Number:        3,
		State:         "closed",
		IsPullRequest: true,
		Merged:        true,
		UpdatedAt:     at(1),
	}, ReasonMine)
	require.Equal(t, "octocat/hello", item.Repo)
	require.Equal(t, TypePullRequest, item.Type)
	require.Equal(t, StateMerged, item.State)
	require.Equal(t, []string{ReasonMine}, item.Reasons)
	require.False(t, item.HasNotification())
}

func TestMergeDedupesAndKeepsEveryReason(t *testing.T) {
	merged := Merge(
		[]Item{notif("t1", 1, "review_requested", at(5)), notif("t2", 2, "mention", at(1))},
		[]Item{pr(1, at(3), ReasonReviewRequested), pr(3, at(2), ReasonReviewRequested)},
		[]Item{pr(2, at(4), ReasonMine)},
		[]Item{pr(1, at(3), ReasonReviewed)},
	)
	require.Len(t, merged, 3)

	one := merged[0]
	require.Equal(t, []string{"review_requested", ReasonReviewed}, one.Reasons)
	require.Equal(t, "t1", one.ThreadID)
	require.Equal(t, at(5), one.UpdatedAt, "keeps the newest updated_at")
	require.Equal(t, StateOpen, one.State, "state comes from the search result")

	two := merged[1]
	require.Equal(t, []string{"mention", ReasonMine}, two.Reasons)
	require.Equal(t, at(4), two.UpdatedAt)

	require.Equal(t, "", merged[2].ThreadID)
}

func TestArrangeGroupsAndSorts(t *testing.T) {
	items := []Item{
		pr(1, at(1), "subscribed"),
		pr(2, at(2), ReasonReviewed),
		pr(3, at(3), ReasonMine),
		pr(4, at(4), ReasonReviewRequested),
		pr(5, at(5), ReasonReviewRequested),
		pr(6, at(6), "mention", ReasonMine),
		pr(7, at(7), ReasonAuthor),
	}
	got := Arrange(items, nil, nil, nil)
	require.Equal(t, keys([]Item{items[4], items[3], items[6], items[5], items[2], items[1], items[0]}), keys(got))
	require.Equal(t, GroupReviewRequested, got[0].Group)
	require.Equal(t, GroupMine, got[2].Group)
	require.Equal(t, GroupMine, got[3].Group, "earliest matching group wins")
	require.Equal(t, GroupReviewed, got[5].Group)
	require.Equal(t, GroupNotifications, got[6].Group)
}

func TestArrangeCustomGroupOrder(t *testing.T) {
	items := []Item{
		pr(1, at(1), ReasonReviewRequested),
		pr(2, at(2), ReasonMine),
		pr(3, at(3), "mention"),
	}
	order := NormalizeGroupOrder([]string{"notifications", "Mine", "bogus"})
	require.Equal(t, []Group{GroupNotifications, GroupMine, GroupReviewRequested, GroupReviewed}, order)
	got := Arrange(items, nil, nil, order)
	require.Equal(t, keys([]Item{items[2], items[1], items[0]}), keys(got))
}

func TestArrangeStableOnTies(t *testing.T) {
	a := Arrange([]Item{pr(2, at(1), ReasonMine), pr(1, at(1), ReasonMine)}, nil, nil, nil)
	b := Arrange([]Item{pr(1, at(1), ReasonMine), pr(2, at(1), ReasonMine)}, nil, nil, nil)
	require.Equal(t, keys(a), keys(b))
}

func TestPinsComeFirstInPinOrderAndSurviveDisappearing(t *testing.T) {
	items := []Item{
		pr(1, at(1), ReasonMine),
		pr(2, at(2), ReasonReviewRequested),
		pr(3, at(3), ReasonMine),
	}
	var pins []Pin
	var pinned bool
	pins, pinned = TogglePin(pins, items[2])
	require.True(t, pinned)
	pins, _ = TogglePin(pins, items[0])
	gone := pr(9, at(0), ReasonMine)
	gone.State = StateMerged
	pins, _ = TogglePin(pins, gone)

	got := Arrange(items, pins, Dismissed{items[0].Key: at(10)}, nil)
	require.Equal(t, keys([]Item{items[2], items[0], gone, items[1]}), keys(got))
	require.True(t, got[0].Pinned)
	require.Equal(t, GroupPinned, got[1].Group, "pins are never hidden by a dismissal")
	require.Equal(t, StateMerged, got[2].State, "a pin missing from the sources is shown from its snapshot")
	require.False(t, got[3].Pinned)

	pins, pinned = TogglePin(pins, items[2])
	require.False(t, pinned)
	require.Len(t, pins, 2)
}

func TestRefreshPins(t *testing.T) {
	old := pr(1, at(1), ReasonMine)
	pins, _ := TogglePin(nil, old)
	fresh := old
	fresh.Title = "Renamed"
	fresh.UpdatedAt = at(2)

	updated, changed := RefreshPins(pins, []Item{fresh})
	require.True(t, changed)
	require.Equal(t, "Renamed", updated[0].Title)

	_, changed = RefreshPins(updated, []Item{fresh})
	require.False(t, changed)
}

func TestDismissedUntilUpdated(t *testing.T) {
	item := pr(1, at(1), ReasonMine)
	dismissed := Dismissed{item.Key: at(1)}

	require.Empty(t, Arrange([]Item{item}, nil, dismissed, nil))

	older := item
	older.UpdatedAt = at(0)
	require.Empty(t, Arrange([]Item{older}, nil, dismissed, nil))

	item.UpdatedAt = at(2)
	require.Len(t, Arrange([]Item{item}, nil, dismissed, nil), 1, "reappears once updated_at moves on")
}

func TestSlotsOf(t *testing.T) {
	var items []Item
	for i := 1; i <= 9; i++ {
		items = append(items, pr(i, at(i), ReasonMine))
	}
	items = append(items, pr(0, at(0), ReasonMine))

	slots := SlotsOf(items)
	require.Len(t, slots, MaxSlots)
	require.Equal(t, 1, slots[0].Slot)
	require.Equal(t, items[8].Key, slots[8].Key)

	require.True(t, SlotsEqual(slots, SlotsOf(items)))
	items[0], items[1] = items[1], items[0]
	require.False(t, SlotsEqual(slots, SlotsOf(items)))
	require.Empty(t, SlotsOf(nil))
}

func TestStateDir(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(k string) string { return values[k] }
	}
	require.Equal(t,
		filepath.Join("/plugin/state"),
		StateDir("github.example.com", env(map[string]string{
			"HERDR_PLUGIN_STATE_DIR": "/plugin/state",
			"XDG_STATE_HOME":         "/xdg",
		}), "/home/octocat"),
	)
	require.Equal(t,
		filepath.Join("/xdg", "gh-dash", "github.example.com"),
		StateDir("github.example.com", env(map[string]string{"XDG_STATE_HOME": "/xdg"}), "/home/octocat"),
	)
	require.Equal(t,
		filepath.Join("/home/octocat", ".local", "state", "gh-dash", "github.com"),
		StateDir("", env(nil), "/home/octocat"),
	)
}

func TestStoreRoundTrip(t *testing.T) {
	store := Store{Dir: filepath.Join(t.TempDir(), "nested", "state")}

	pins, err := store.LoadPins()
	require.NoError(t, err)
	require.Empty(t, pins)
	_, err = store.ReadSlots()
	require.True(t, errors.Is(err, ErrNoSlots))

	item := pr(1, at(1), ReasonMine)
	pins, _ = TogglePin(pins, item)
	require.NoError(t, store.SavePins(pins))
	loaded, err := store.LoadPins()
	require.NoError(t, err)
	require.Equal(t, item.Key, loaded[0].Key)
	require.True(t, loaded[0].UpdatedAt.Equal(item.UpdatedAt))

	require.NoError(t, store.Dismiss(item))
	other := pr(2, at(2), ReasonMine)
	require.NoError(t, store.Dismiss(other))
	dismissed, err := store.LoadDismissed()
	require.NoError(t, err)
	require.True(t, dismissed.Hides(item))
	require.True(t, dismissed.Hides(other))

	require.NoError(t, store.WriteSlots("github.example.com", SlotsOf([]Item{item, other}), at(3)))
	doc, err := store.ReadSlots()
	require.NoError(t, err)
	require.Equal(t, "github.example.com", doc.Host)
	require.True(t, doc.GeneratedAt.Equal(at(3)))

	found, err := doc.Find(2)
	require.NoError(t, err)
	require.Equal(t, other.Title, found.Title)
	_, err = doc.Find(3)
	require.ErrorContains(t, err, "slot 3 is empty")
	_, err = doc.Find(10)
	require.Error(t, err)

	entries, err := os.ReadDir(store.Dir)
	require.NoError(t, err)
	require.Len(t, entries, 3, "no temp files are left behind")
}
