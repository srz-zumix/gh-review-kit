package commentarg

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		wantID  int64
		wantPR  int
		wantErr bool
	}{
		{
			name:   "bare comment ID",
			arg:    "123456789",
			wantID: 123456789,
			wantPR: 0,
		},
		{
			name:   "review comment URL",
			arg:    "https://github.com/srz-zumix/go-gh-extension/pull/331#discussion_r4016555925",
			wantID: 4016555925,
			wantPR: 331,
		},
		{
			name:    "invalid arg",
			arg:     "not-a-comment",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotPR, err := Parse(tt.arg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if gotID != tt.wantID {
				t.Errorf("Parse() commentID = %v, want %v", gotID, tt.wantID)
			}
			if gotPR != tt.wantPR {
				t.Errorf("Parse() prNumber = %v, want %v", gotPR, tt.wantPR)
			}
		})
	}
}
