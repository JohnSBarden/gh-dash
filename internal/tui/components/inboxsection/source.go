package inboxsection

import (
	"sync"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/inbox"
)

// The searches merged into the inbox next to the notifications.
const (
	QueryReviewRequested = "is:open review-requested:@me"
	QueryMine            = "is:open author:@me"
	QueryReviewed        = "is:open reviewed-by:@me -author:@me"
)

// Source is where the inbox gets its data. It is an interface so the section
// can be tested without the network.
type Source interface {
	Notifications(limit int) ([]data.NotificationData, error)
	Search(query string, limit int) ([]data.SearchIssue, error)
	Subject(repo string, number int) (data.SearchIssue, error)
	MarkDone(threadID string) error
}

// GitHubSource talks to the host the gh CLI is configured for.
type GitHubSource struct{}

func (GitHubSource) Notifications(limit int) ([]data.NotificationData, error) {
	res, err := data.FetchNotifications(limit, nil, data.NotificationStateUnread, nil)
	return res.Notifications, err
}

func (GitHubSource) Search(query string, limit int) ([]data.SearchIssue, error) {
	return data.SearchIssues(query, limit)
}

func (GitHubSource) Subject(repo string, number int) (data.SearchIssue, error) {
	return data.FetchIssueOrPullRequest(repo, number)
}

func (GitHubSource) MarkDone(threadID string) error {
	return data.MarkNotificationDone(threadID)
}

func fromNotifications(notifications []data.NotificationData) []inbox.Item {
	items := make([]inbox.Item, 0, len(notifications))
	for _, n := range notifications {
		items = append(items, inbox.FromNotification(inbox.NotificationInput{
			ThreadID:    n.Id,
			Reason:      n.Reason,
			Unread:      n.Unread,
			UpdatedAt:   n.UpdatedAt,
			Title:       n.Subject.Title,
			SubjectType: n.Subject.Type,
			SubjectURL:  n.Subject.Url,
			Repo:        n.Repository.FullName,
			RepoURL:     n.GetUrl(),
		}))
	}
	return items
}

func fromSearch(issue data.SearchIssue, reason string) inbox.Item {
	return inbox.FromSearch(inbox.SearchInput{
		HTMLURL:       issue.HtmlUrl,
		Title:         issue.Title,
		Number:        issue.Number,
		State:         issue.State,
		Draft:         issue.Draft,
		IsPullRequest: issue.IsPullRequest(),
		Merged:        issue.IsMerged(),
		UpdatedAt:     issue.UpdatedAt,
	}, reason)
}

// Collect fetches every source concurrently and merges the result. Pinned
// items that no source returns any more (merged or closed ones) are looked up
// individually so their row shows the current state.
func Collect(src Source, limit int, pins []inbox.Pin) ([]inbox.Item, error) {
	searches := []struct {
		query  string
		reason string
	}{
		{QueryReviewRequested, inbox.ReasonReviewRequested},
		{QueryMine, inbox.ReasonMine},
		{QueryReviewed, inbox.ReasonReviewed},
	}

	results := make([][]inbox.Item, len(searches)+1)
	errs := make([]error, len(searches)+1)
	var wg sync.WaitGroup
	for i, search := range searches {
		wg.Go(func() {
			issues, err := src.Search(search.query, limit)
			errs[i] = err
			for _, issue := range issues {
				results[i] = append(results[i], fromSearch(issue, search.reason))
			}
		})
	}
	wg.Go(func() {
		notifications, err := src.Notifications(limit)
		errs[len(searches)] = err
		results[len(searches)] = fromNotifications(notifications)
	})
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	merged := inbox.Merge(results...)
	known := make(map[string]bool, len(merged))
	for _, item := range merged {
		known[item.Key] = true
	}
	for _, pin := range pins {
		if known[pin.Key] || pin.Repo == "" || pin.Number == 0 {
			continue
		}
		issue, err := src.Subject(pin.Repo, pin.Number)
		if err != nil || issue.HtmlUrl == "" {
			continue
		}
		item := fromSearch(issue, "")
		item.Key = pin.Key
		merged = append(merged, item)
	}
	return merged, nil
}
