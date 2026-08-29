package attestation

import "testing"

func TestOrderedKnownTags(t *testing.T) {
	tests := []struct {
		name  string
		found map[string]string
		want  []Tag
	}{
		{
			name:  "nil map returns nil",
			found: nil,
			want:  nil,
		},
		{
			name:  "empty comment is omitted",
			found: map[string]string{GitTagBranch: "main", CommentTag: ""},
			want:  []Tag{{Key: GitTagBranch, Value: "main"}},
		},
		{
			name:  "non-empty comment is retained last",
			found: map[string]string{GitTagBranch: "main", CommentTag: "note"},
			want: []Tag{
				{Key: GitTagBranch, Value: "main"},
				{Key: CommentTag, Value: "note"},
			},
		},
		{
			name:  "empty provenance value is retained",
			found: map[string]string{GitTagBranch: ""},
			want:  []Tag{{Key: GitTagBranch, Value: ""}},
		},
		{
			name: "known ordering is preserved regardless of map order",
			found: map[string]string{
				GitTagRepository: "github.com/owner/repo",
				GitTagCommit:     "abc123",
				GitTagBranch:     "main",
			},
			want: []Tag{
				{Key: GitTagCommit, Value: "abc123"},
				{Key: GitTagBranch, Value: "main"},
				{Key: GitTagRepository, Value: "github.com/owner/repo"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orderedKnownTags(tt.found)
			if len(got) != len(tt.want) {
				t.Fatalf("orderedKnownTags = %v, want %v", got, tt.want)
			}
			for i, tag := range tt.want {
				if got[i] != tag {
					t.Fatalf("tag[%d] = %v, want %v", i, got[i], tag)
				}
			}
		})
	}
}
