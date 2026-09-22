package copilot

import "testing"

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
