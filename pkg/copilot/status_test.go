package copilot

import (
	"testing"

	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

func TestDeriveStatus(t *testing.T) {
	tests := []struct {
		name            string
		requested       bool
		lastReviewState string
		want            Status
	}{
		{"never requested", false, "", StatusNotRequested},
		{"requested", true, "", StatusInProgress},
		{"re-requested after a review", true, gh.PullRequestReviewStateCommented, StatusInProgress},
		{"commented", false, gh.PullRequestReviewStateCommented, StatusCommented},
		{"approved", false, gh.PullRequestReviewStateApproved, StatusApproved},
		{"changes requested", false, gh.PullRequestReviewStateChangesRequested, StatusChangesRequested},
		{"dismissed", false, gh.PullRequestReviewStateDismissed, StatusDismissed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveStatus(tt.requested, tt.lastReviewState); got != tt.want {
				t.Errorf("deriveStatus(%t, %q) = %q, want %q", tt.requested, tt.lastReviewState, got, tt.want)
			}
		})
	}
}

func TestIsCopilotLogin(t *testing.T) {
	tests := []struct {
		login string
		want  bool
	}{
		{DefaultAuthor, true},
		{ReviewerLogin, true},
		{"Copilot-Pull-Request-Reviewer[bot]", true},
		{"dependabot[bot]", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.login, func(t *testing.T) {
			if got := isCopilotLogin(tt.login); got != tt.want {
				t.Errorf("isCopilotLogin(%q) = %t, want %t", tt.login, got, tt.want)
			}
		})
	}
}
