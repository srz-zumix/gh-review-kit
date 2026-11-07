package copilot

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestListCommentsKeepsThreadRootBeforeAuthorFiltering(t *testing.T) {
	g := newReplyTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/graphql" {
			t.Errorf("unexpected API path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
			{"id":"THREAD_1","isResolved":false,"isOutdated":false,"comments":{"nodes":[
				{"databaseId":11,"body":"root","author":{"login":"example-reviewer","__typename":"User"}},
				{"databaseId":22,"body":"reply","author":{"login":%q,"__typename":"Bot"}}
			],"pageInfo":{"hasNextPage":false,"endCursor":null}}},
			{"id":"THREAD_2","isResolved":false,"isOutdated":false,"comments":{"nodes":[
				{"databaseId":33,"body":"other root","author":{"login":%q,"__typename":"Bot"}}
			],"pageInfo":{"hasNextPage":false,"endCursor":null}}}
		],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`, DefaultAuthor, DefaultAuthor)
	}, false)
	comments, err := ListComments(context.Background(), g, replyTestRepo, 7, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 {
		t.Fatalf("comments = %+v, want two matching authors", comments)
	}
	if comments[0].CommentID != 22 || comments[0].RootCommentID != 11 || comments[0].ThreadID != "THREAD_1" {
		t.Errorf("reply comment has wrong root: %+v", comments[0])
	}
	if comments[1].CommentID != 33 || comments[1].RootCommentID != 33 || comments[1].ThreadID != "THREAD_2" {
		t.Errorf("root comment has wrong root: %+v", comments[1])
	}
}

func TestIsAuthorMatch(t *testing.T) {
	tests := []struct {
		name    string
		author  string
		authors []string
		want    bool
	}{
		{"exact match", DefaultAuthor, []string{DefaultAuthor}, true},
		{"case insensitive", "Copilot-Pull-Request-Reviewer", []string{DefaultAuthor}, true},
		{"no match", "someone-else", []string{DefaultAuthor}, false},
		{"empty authors", DefaultAuthor, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAuthorMatch(tt.author, tt.authors); got != tt.want {
				t.Errorf("isAuthorMatch(%q, %v) = %v, want %v", tt.author, tt.authors, got, tt.want)
			}
		})
	}
}
