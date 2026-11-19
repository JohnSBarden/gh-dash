package data

import (
	"fmt"
	"net/url"
	"time"

	"charm.land/log/v2"
)

// SearchIssue is an issue or pull request as returned by the REST issue
// search and issue endpoints. It is deliberately small: the inbox only needs
// enough to list an item.
type SearchIssue struct {
	HtmlUrl     string    `json:"html_url"`
	Title       string    `json:"title"`
	Number      int       `json:"number"`
	State       string    `json:"state"`
	Draft       bool      `json:"draft"`
	UpdatedAt   time.Time `json:"updated_at"`
	PullRequest *struct {
		MergedAt *time.Time `json:"merged_at"`
	} `json:"pull_request"`
}

func (s SearchIssue) IsPullRequest() bool {
	return s.PullRequest != nil
}

func (s SearchIssue) IsMerged() bool {
	return s.PullRequest != nil && s.PullRequest.MergedAt != nil
}

type searchIssuesResponse struct {
	TotalCount int           `json:"total_count"`
	Items      []SearchIssue `json:"items"`
}

// SearchIssuesPath builds the REST path of an issue search.
func SearchIssuesPath(query string, limit int) string {
	return fmt.Sprintf(
		"search/issues?q=%s&per_page=%d&sort=updated&order=desc",
		url.QueryEscape(query),
		limit,
	)
}

// SearchIssues runs an issue search (issues and pull requests) on the
// configured host, newest updated first.
func SearchIssues(query string, limit int) ([]SearchIssue, error) {
	client, err := getRESTClient()
	if err != nil {
		return nil, err
	}

	log.Debug("Searching issues", "query", query, "limit", limit)
	var response searchIssuesResponse
	if err := client.Get(SearchIssuesPath(query, limit), &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}

// FetchIssueOrPullRequest fetches a single issue or pull request by repo
// (owner/name) and number.
func FetchIssueOrPullRequest(repo string, number int) (SearchIssue, error) {
	client, err := getRESTClient()
	if err != nil {
		return SearchIssue{}, err
	}

	var issue SearchIssue
	err = client.Get(fmt.Sprintf("repos/%s/issues/%d", repo, number), &issue)
	return issue, err
}
