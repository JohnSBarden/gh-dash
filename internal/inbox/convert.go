package inbox

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// NotificationInput is the subset of a GitHub notification the inbox needs.
type NotificationInput struct {
	ThreadID    string
	Reason      string
	Unread      bool
	UpdatedAt   time.Time
	Title       string
	SubjectType string
	SubjectURL  string // API URL of the subject, may be empty
	Repo        string // owner/name
	RepoURL     string // web URL of the repository
}

// FromNotification converts a notification into an inbox item.
func FromNotification(n NotificationInput) Item {
	base := strings.TrimRight(n.RepoURL, "/")
	item := Item{
		Title:     cleanTitle(n.Title),
		Repo:      n.Repo,
		Type:      n.SubjectType,
		UpdatedAt: n.UpdatedAt,
		Reasons:   []string{n.Reason},
		ThreadID:  n.ThreadID,
		Unread:    n.Unread,
	}
	last := lastSegment(n.SubjectURL)
	number, numErr := strconv.Atoi(last)
	switch {
	case n.SubjectType == TypePullRequest && numErr == nil && base != "":
		item.Number = number
		item.URL = base + "/pull/" + last
		item.Key = item.URL
	case n.SubjectType == TypeIssue && numErr == nil && base != "":
		item.Number = number
		item.URL = base + "/issues/" + last
		item.Key = item.URL
	default:
		// Discussions, releases, check suites and the like have no stable
		// web URL in the payload, so they are keyed by thread.
		item.URL = base
		item.Key = "thread:" + n.ThreadID
	}
	return item
}

// SearchInput is the subset of an issue search result the inbox needs.
type SearchInput struct {
	HTMLURL       string
	Title         string
	Number        int
	State         string // open | closed
	Draft         bool
	IsPullRequest bool
	Merged        bool
	UpdatedAt     time.Time
}

// FromSearch converts a search result into an inbox item carrying reason.
func FromSearch(s SearchInput, reason string) Item {
	item := Item{
		Key:       strings.TrimRight(s.HTMLURL, "/"),
		URL:       strings.TrimRight(s.HTMLURL, "/"),
		Title:     cleanTitle(s.Title),
		Repo:      RepoFromURL(s.HTMLURL),
		Number:    s.Number,
		Type:      TypeIssue,
		State:     strings.ToLower(s.State),
		Draft:     s.Draft,
		UpdatedAt: s.UpdatedAt,
		Reasons:   []string{},
	}
	if reason != "" {
		item.Reasons = append(item.Reasons, reason)
	}
	if s.IsPullRequest {
		item.Type = TypePullRequest
	}
	if s.Merged {
		item.State = StateMerged
	}
	return item
}

// RepoFromURL extracts owner/name from a subject web URL such as
// https://github.example.com/acme/widgets/pull/7.
func RepoFromURL(htmlURL string) string {
	u, err := url.Parse(htmlURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// HostFromURL returns the host of a URL, or "" when it cannot be parsed.
func HostFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func lastSegment(raw string) string {
	raw = strings.TrimRight(raw, "/")
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		return raw[i+1:]
	}
	return ""
}

func cleanTitle(title string) string {
	title = strings.ReplaceAll(title, "\r", "")
	title = strings.ReplaceAll(title, "\n", " ")
	return strings.TrimSpace(title)
}
