package data

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchIssuesPath(t *testing.T) {
	require.Equal(
		t,
		"search/issues?q=is%3Aopen+reviewed-by%3A%40me+-author%3A%40me&per_page=50&sort=updated&order=desc",
		SearchIssuesPath("is:open reviewed-by:@me -author:@me", 50),
	)
}

func TestSearchIssueDecoding(t *testing.T) {
	raw := `{"total_count": 2, "items": [
		{"html_url": "https://github.example.com/acme/widgets/pull/7", "title": "Add gears",
		 "number": 7, "state": "closed", "draft": false, "updated_at": "2025-01-10T12:00:00Z",
		 "pull_request": {"merged_at": "2025-01-10T11:00:00Z"}},
		{"html_url": "https://github.example.com/acme/widgets/issues/8", "title": "Gears squeak",
		 "number": 8, "state": "open", "updated_at": "2025-01-09T12:00:00Z"}
	]}`
	var response searchIssuesResponse
	require.NoError(t, json.Unmarshal([]byte(raw), &response))
	require.Len(t, response.Items, 2)
	require.True(t, response.Items[0].IsPullRequest())
	require.True(t, response.Items[0].IsMerged())
	require.False(t, response.Items[1].IsPullRequest())
	require.False(t, response.Items[1].IsMerged())
}
