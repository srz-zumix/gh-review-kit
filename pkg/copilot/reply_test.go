package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/gh/client"
	"github.com/srz-zumix/go-gh-extension/pkg/gh/factory"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

var replyTestRepo = repository.Repository{Owner: "example-org", Name: "example-repo"}

func newReplyTestClient(t *testing.T, handler http.HandlerFunc, readOnly bool) *gh.GitHubClient {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	rest, err := factory.NewGithubClient(
		factory.Endpoint(server.URL),
		factory.HTTPClient(server.Client()),
		factory.SkipAuth(true),
		factory.ReadOnly(readOnly),
	)
	if err != nil {
		t.Fatal(err)
	}
	g, err := client.NewClient(rest)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func fakeReplyEvaluator(t *testing.T, evaluator Evaluator, batch bool, verdict Verdict, reason string) string {
	t.Helper()
	var result any = Evaluation{Verdict: verdict, Reason: reason}
	if batch {
		result = []batchEvaluation{{CommentID: "22", Verdict: verdict, Reason: reason}}
	}
	output, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if evaluator.IsClaude() {
		output, err = json.Marshal(claudeResult{Type: "result", Result: string(output)})
		if err != nil {
			t.Fatal(err)
		}
	}
	return fakeCopilotBin(t, string(output), 0)
}

func TestValidateReplyReasonOption(t *testing.T) {
	for _, tt := range []struct {
		evaluate    bool
		replyReason bool
		wantErr     bool
	}{
		{false, false, false},
		{true, false, false},
		{true, true, false},
		{false, true, true},
	} {
		err := ValidateReplyReasonOption(tt.evaluate, tt.replyReason)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateReplyReasonOption(%t, %t) error = %v, wantErr %t", tt.evaluate, tt.replyReason, err, tt.wantErr)
		}
	}
}

func TestEvaluateAllRepliesBeforeVerdict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake evaluator requires a POSIX shell")
	}
	const reason = "  Verified against the patch.\nSecond line.\n"
	const replyURL = "https://example.test/example-org/example-repo/pull/7#discussion_r301"
	for _, verdict := range []struct {
		value      Verdict
		resolution string
		action     Action
	}{
		{VerdictValid, "ADDRESSED", ActionResolved},
		{VerdictInvalid, "INVALID", ActionFeedback},
		{VerdictUnclear, "WONT_FIX", ActionResolved},
	} {
		for _, evaluator := range []Evaluator{EvaluatorCopilot, EvaluatorClaude} {
			for _, batch := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/batch=%t", verdict.value, evaluator, batch), func(t *testing.T) {
					var mu sync.Mutex
					var requests []string
					g := newReplyTestClient(t, func(w http.ResponseWriter, r *http.Request) {
						mu.Lock()
						requests = append(requests, r.URL.Path)
						mu.Unlock()
						w.Header().Set("Content-Type", "application/json")
						if r.Method != http.MethodPost {
							t.Errorf("unexpected method: %s", r.Method)
							http.Error(w, "unexpected method", http.StatusBadRequest)
							return
						}
						switch r.URL.Path {
						case "/repos/example-org/example-repo/pulls/7/comments":
							var body map[string]any
							if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
								t.Error(err)
							}
							wantBody := map[string]any{"body": reason, "in_reply_to": float64(11)}
							if !reflect.DeepEqual(body, wantBody) {
								t.Errorf("reply payload = %+v, want %+v", body, wantBody)
								http.Error(w, `{"message":"Validation Failed","errors":[{"resource":"PullRequestReviewComment","field":"commit_id","code":"invalid"}]}`, http.StatusUnprocessableEntity)
								return
							}
							w.WriteHeader(http.StatusCreated)
							_, _ = fmt.Fprintf(w, `{"id":301,"html_url":%q}`, replyURL)
						case "/repos/example-org/example-repo/pulls/comments/22/reactions":
							var body struct {
								Content string `json:"content"`
							}
							if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
								t.Error(err)
							}
							if body.Content != "-1" {
								t.Errorf("reaction = %q, want -1", body.Content)
							}
							w.WriteHeader(http.StatusCreated)
							_, _ = io.WriteString(w, `{"content":"-1"}`)
						case "/api/graphql":
							body, err := io.ReadAll(r.Body)
							if err != nil {
								t.Error(err)
							}
							if !strings.Contains(string(body), verdict.resolution) || !strings.Contains(string(body), "THREAD_1") {
								t.Errorf("unexpected resolution mutation: %s", body)
							}
							_, _ = io.WriteString(w, `{"data":{"resolveReviewThread":{"thread":{"id":"THREAD_1","isResolved":true}}}}`)
						default:
							t.Errorf("unexpected API path: %s", r.URL.Path)
							http.NotFound(w, r)
						}
					}, false)
					opts := EvaluateOptions{
						Evaluator:   evaluator,
						Bin:         fakeReplyEvaluator(t, evaluator, batch, verdict.value, reason),
						Prompt:      "judge",
						ReplyReason: true,
					}
					comments := []*Comment{{CommentID: 22, RootCommentID: 11, ThreadID: "THREAD_1"}}
					results, _, err := EvaluateAll(context.Background(), g, replyTestRepo, opts, "example-org/example-repo", 7, comments, batch, false)
					if err != nil {
						t.Fatal(err)
					}
					wantReply := &EvaluationReply{Status: ReplyPosted, CommentID: 301, URL: replyURL}
					if len(results) != 1 || results[0].Action != verdict.action || !reflect.DeepEqual(results[0].Reply, wantReply) {
						t.Fatalf("results = %+v, want action %q and reply %+v", results, verdict.action, wantReply)
					}
					wantRequests := []string{"/repos/example-org/example-repo/pulls/7/comments"}
					if verdict.value == VerdictInvalid {
						wantRequests = append(wantRequests, "/repos/example-org/example-repo/pulls/comments/22/reactions")
					}
					wantRequests = append(wantRequests, "/api/graphql")
					mu.Lock()
					defer mu.Unlock()
					if !reflect.DeepEqual(requests, wantRequests) {
						t.Errorf("requests = %v, want %v", requests, wantRequests)
					}
				})
			}
		}
	}
}

func TestEvaluateAllReplyDryRunAndValidation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake evaluator requires a POSIX shell")
	}
	for _, tt := range []struct {
		name       string
		verdict    Verdict
		reason     string
		rootID     int64
		threadID   string
		dryRun     bool
		wantAction Action
		wantStatus ReplyStatus
		wantError  string
	}{
		{"valid dry run", VerdictValid, "reason", 11, "THREAD_1", true, ActionResolved, ReplyPlanned, ""},
		{"invalid dry run", VerdictInvalid, "reason", 11, "THREAD_1", true, ActionFeedback, ReplyPlanned, ""},
		{"unclear dry run", VerdictUnclear, "reason", 11, "THREAD_1", true, ActionResolved, ReplyPlanned, ""},
		{"empty reason", VerdictValid, "", 11, "THREAD_1", false, ActionNone, ReplyFailed, "reason is empty"},
		{"whitespace reason", VerdictValid, " \n\t", 11, "THREAD_1", true, ActionNone, ReplyFailed, "reason is empty"},
		{"missing root", VerdictValid, "reason", 0, "THREAD_1", false, ActionNone, ReplyFailed, "root comment ID is missing"},
		{"missing root in dry run", VerdictValid, "reason", 0, "THREAD_1", true, ActionNone, ReplyFailed, "root comment ID is missing"},
		{"missing thread", VerdictValid, "reason", 11, "", false, ActionNone, ReplyFailed, "thread ID is missing"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := EvaluateOptions{Bin: fakeReplyEvaluator(t, EvaluatorCopilot, true, tt.verdict, tt.reason), Prompt: "judge", ReplyReason: true}
			comments := []*Comment{{CommentID: 22, RootCommentID: tt.rootID, ThreadID: tt.threadID}}
			results, _, err := EvaluateAll(context.Background(), nil, replyTestRepo, opts, "example-org/example-repo", 7, comments, true, tt.dryRun)
			if (err != nil) != (tt.wantError != "") || (err != nil && !strings.Contains(err.Error(), tt.wantError)) {
				t.Fatalf("EvaluateAll() error = %v, want %q", err, tt.wantError)
			}
			if len(results) != 1 || results[0].Action != tt.wantAction || results[0].Reply == nil || results[0].Reply.Status != tt.wantStatus {
				t.Fatalf("results = %+v, want action %q and reply %q", results, tt.wantAction, tt.wantStatus)
			}
		})
	}
}

func TestEvaluateAllReplyFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake evaluator requires a POSIX shell")
	}
	for _, tt := range []struct {
		name       string
		failAt     string
		wantStatus ReplyStatus
		wantPaths  []string
	}{
		{"posting fails", "reply", ReplyFailed, []string{"reply"}},
		{"missing reply details", "details", ReplyPosted, []string{"reply"}},
		{"reaction fails after posting", "reaction", ReplyPosted, []string{"reply", "reaction"}},
		{"resolution fails after posting", "resolve", ReplyPosted, []string{"reply", "reaction", "resolve"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var events []string
			g := newReplyTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				event := ""
				switch r.URL.Path {
				case "/repos/example-org/example-repo/pulls/7/comments":
					event = "reply"
				case "/repos/example-org/example-repo/pulls/comments/22/reactions":
					event = "reaction"
				case "/api/graphql":
					event = "resolve"
				default:
					t.Errorf("unexpected API path: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				mu.Lock()
				events = append(events, event)
				mu.Unlock()
				if event == tt.failAt {
					http.Error(w, `{"message":"refused"}`, http.StatusForbidden)
					return
				}
				switch event {
				case "reply":
					w.WriteHeader(http.StatusCreated)
					if tt.failAt == "details" {
						_, _ = io.WriteString(w, `null`)
					} else {
						_, _ = io.WriteString(w, `{"id":301,"html_url":"https://example.test/reply"}`)
					}
				case "reaction":
					w.WriteHeader(http.StatusCreated)
					_, _ = io.WriteString(w, `{"content":"-1"}`)
				case "resolve":
					_, _ = io.WriteString(w, `{"data":{"resolveReviewThread":{"thread":{"id":"THREAD_1","isResolved":true}}}}`)
				}
			}, false)
			opts := EvaluateOptions{Bin: fakeReplyEvaluator(t, EvaluatorCopilot, true, VerdictInvalid, "reason"), Prompt: "judge", ReplyReason: true}
			results, _, err := EvaluateAll(context.Background(), g, replyTestRepo, opts, "example-org/example-repo", 7, []*Comment{{CommentID: 22, RootCommentID: 11, ThreadID: "THREAD_1"}}, true, false)
			if err == nil || !strings.Contains(err.Error(), "comment 22") {
				t.Fatalf("EvaluateAll() error = %v, want per-comment failure", err)
			}
			if len(results) != 1 || results[0].Action != ActionNone || results[0].Reply == nil || results[0].Reply.Status != tt.wantStatus || results[0].Error == "" {
				t.Fatalf("unexpected result: %+v", results)
			}
			if tt.wantStatus == ReplyPosted && tt.failAt != "details" && (results[0].Reply.CommentID != 301 || results[0].Reply.URL != "https://example.test/reply") {
				t.Errorf("posted reply details lost: %+v", results[0].Reply)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(events, tt.wantPaths) {
				t.Errorf("events = %v, want %v", events, tt.wantPaths)
			}
		})
	}
}

func TestEvaluateAllContinuesAfterReplyFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake evaluator requires a POSIX shell")
	}
	output := `[{"comment_id":22,"verdict":"invalid","reason":"first"},{"comment_id":23,"verdict":"unclear","reason":"second"}]`
	var mu sync.Mutex
	var events []string
	g := newReplyTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/example-org/example-repo/pulls/7/comments":
			var body struct {
				Body      string `json:"body"`
				InReplyTo int64  `json:"in_reply_to"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			mu.Lock()
			events = append(events, "reply:"+body.Body)
			mu.Unlock()
			if body.Body == "first" && body.InReplyTo == 11 {
				http.Error(w, `{"message":"refused"}`, http.StatusForbidden)
				return
			}
			if body.Body != "second" || body.InReplyTo != 33 {
				t.Errorf("unexpected reply payload: %+v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":301,"html_url":"https://example.test/reply"}`)
		case "/api/graphql":
			mu.Lock()
			events = append(events, "resolve")
			mu.Unlock()
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			if !strings.Contains(string(body), "THREAD_2") || !strings.Contains(string(body), "WONT_FIX") {
				t.Errorf("unexpected resolution mutation: %s", body)
			}
			_, _ = io.WriteString(w, `{"data":{"resolveReviewThread":{"thread":{"id":"THREAD_2","isResolved":true}}}}`)
		default:
			t.Errorf("unexpected API path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}, false)
	opts := EvaluateOptions{Bin: fakeCopilotBin(t, output, 0), Prompt: "judge", ReplyReason: true}
	comments := []*Comment{
		{CommentID: 22, RootCommentID: 11, ThreadID: "THREAD_1"},
		{CommentID: 23, RootCommentID: 33, ThreadID: "THREAD_2"},
	}
	results, _, err := EvaluateAll(context.Background(), g, replyTestRepo, opts, "example-org/example-repo", 7, comments, true, false)
	if err == nil || !strings.Contains(err.Error(), "comment 22") || len(results) != 2 {
		t.Fatalf("results = %+v, error = %v; want one reported failure", results, err)
	}
	if results[0].Reply.Status != ReplyFailed || results[0].Action != ActionNone || results[1].Reply.Status != ReplyPosted || results[1].Action != ActionResolved {
		t.Fatalf("unexpected per-comment outcomes: %+v", results)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"reply:first", "reply:second", "resolve"}; !reflect.DeepEqual(events, want) {
		t.Errorf("events = %v, want %v", events, want)
	}
}

func TestEvaluateAllReplyReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake evaluator requires a POSIX shell")
	}
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dryrun=%t", dryRun), func(t *testing.T) {
			var requests atomic.Int32
			g := newReplyTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.Error(w, "unexpected request", http.StatusInternalServerError)
			}, true)
			opts := EvaluateOptions{Bin: fakeReplyEvaluator(t, EvaluatorCopilot, true, VerdictValid, "reason"), Prompt: "judge", ReplyReason: true}
			results, _, err := EvaluateAll(context.Background(), g, replyTestRepo, opts, "example-org/example-repo", 7, []*Comment{{CommentID: 22, RootCommentID: 11, ThreadID: "THREAD_1"}}, true, dryRun)
			if dryRun {
				if err != nil || results[0].Reply.Status != ReplyPlanned || results[0].Action != ActionResolved {
					t.Fatalf("dry run result = %+v, error = %v", results, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "only GET and HEAD") || results[0].Reply.Status != ReplyFailed || results[0].Action != ActionNone {
				t.Fatalf("read-only result = %+v, error = %v", results, err)
			}
			if requests.Load() != 0 {
				t.Errorf("read-only client sent %d requests", requests.Load())
			}
		})
	}
}

func TestEvaluateAllReplyAfterBatchRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake evaluator requires a POSIX shell")
	}
	for _, tt := range []struct {
		name        string
		failedReply string
		missing     bool
	}{
		{name: "both replies succeed"},
		{name: "first reply fails", failedReply: "first"},
		{name: "second reply fails", failedReply: "recovered"},
		{name: "second evaluation remains missing", missing: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			files := map[string]string{
				"first":  `[{"comment_id":22,"verdict":"invalid","reason":"first"},{"comment_id":23,"verdict":"valid","reason":"duplicate"},{"comment_id":23,"verdict":"invalid","reason":"duplicate"}]`,
				"second": `[{"comment_id":22,"verdict":"valid","reason":"overwrite"},{"comment_id":23,"verdict":"valid","reason":"recovered"}]`,
			}
			if tt.missing {
				files["second"] = `[{"comment_id":99,"verdict":"valid","reason":"unrelated"}]`
			}
			for name, content := range files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			bin := filepath.Join(dir, "copilot")
			script := "#!/bin/sh\ncd \"$(dirname \"$0\")\"\nif test -f seen; then cat second; else : > seen; cat first; fi\n"
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var events []string
			g := newReplyTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/repos/example-org/example-repo/pulls/7/comments":
					var body struct {
						Body      string `json:"body"`
						InReplyTo int64  `json:"in_reply_to"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body.InReplyTo != 11 {
						t.Errorf("reply targeted %d, want thread root 11", body.InReplyTo)
					}
					mu.Lock()
					events = append(events, "reply:"+body.Body)
					mu.Unlock()
					if body.Body == tt.failedReply {
						http.Error(w, `{"message":"refused"}`, http.StatusForbidden)
						return
					}
					w.WriteHeader(http.StatusCreated)
					_, _ = io.WriteString(w, `{"id":301,"html_url":"https://example.test/reply"}`)
				case "/repos/example-org/example-repo/pulls/comments/22/reactions":
					mu.Lock()
					events = append(events, "reaction")
					mu.Unlock()
					w.WriteHeader(http.StatusCreated)
					_, _ = io.WriteString(w, `{"content":"-1"}`)
				case "/api/graphql":
					mu.Lock()
					events = append(events, "resolve")
					mu.Unlock()
					_, _ = io.WriteString(w, `{"data":{"resolveReviewThread":{"thread":{"id":"THREAD_1","isResolved":true}}}}`)
				default:
					t.Errorf("unexpected API path: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}, false)
			opts := EvaluateOptions{Bin: bin, Prompt: "judge", ReplyReason: true}
			comments := []*Comment{
				{CommentID: 22, RootCommentID: 11, ThreadID: "THREAD_1"},
				{CommentID: 23, RootCommentID: 11, ThreadID: "THREAD_1"},
			}
			results, _, err := EvaluateAll(context.Background(), g, replyTestRepo, opts, "example-org/example-repo", 7, comments, true, false)
			wantFailure := tt.failedReply != "" || tt.missing
			if (err != nil) != wantFailure {
				t.Fatalf("EvaluateAll() error = %v, want failure %t", err, wantFailure)
			}
			if len(results) != 2 || results[0].Evaluation == nil || results[0].Evaluation.Reason != "first" || results[0].Reply == nil {
				t.Fatalf("unexpected recovered results: %+v", results)
			}
			wantEvents := []string{"reply:first"}
			if tt.missing {
				if results[1].Evaluation != nil || results[1].Reply != nil || results[1].Error == "" {
					t.Fatalf("missing evaluation was not reported: %+v", results[1])
				}
			} else {
				if results[1].Evaluation == nil || results[1].Evaluation.Reason != "recovered" || results[1].Reply == nil {
					t.Fatalf("recovered evaluation was lost: %+v", results[1])
				}
				wantEvents = append(wantEvents, "reply:recovered")
			}
			if wantFailure {
				for _, result := range results {
					if result.Evaluation != nil && (result.Action != ActionNone || result.Error == "") {
						t.Errorf("thread action was not held after a sibling failure: %+v", result)
					}
				}
				for _, result := range results {
					if result.Reply == nil {
						continue
					}
					wantStatus := ReplyPosted
					if result.Evaluation.Reason == tt.failedReply {
						wantStatus = ReplyFailed
					}
					if result.Reply.Status != wantStatus {
						t.Errorf("reply status = %q, want %q", result.Reply.Status, wantStatus)
					}
				}
			} else {
				if results[0].Reply.Status != ReplyPosted || results[0].Action != ActionFeedback || results[1].Reply.Status != ReplyPosted || results[1].Action != ActionResolved {
					t.Fatalf("successful results were not applied: %+v", results)
				}
				wantEvents = append(wantEvents, "reaction", "resolve", "resolve")
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(events, wantEvents) {
				t.Errorf("events = %v, want %v", events, wantEvents)
			}
		})
	}
}

func TestEvaluateAllReplyDryRunHoldsThreadOnInvalidReason(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake evaluator requires a POSIX shell")
	}
	output := `[{"comment_id":22,"verdict":"invalid","reason":"first"},{"comment_id":23,"verdict":"valid","reason":" "}]`
	opts := EvaluateOptions{Bin: fakeCopilotBin(t, output, 0), Prompt: "judge", ReplyReason: true}
	comments := []*Comment{
		{CommentID: 22, RootCommentID: 11, ThreadID: "THREAD_1"},
		{CommentID: 23, RootCommentID: 11, ThreadID: "THREAD_1"},
	}
	results, _, err := EvaluateAll(context.Background(), nil, replyTestRepo, opts, "example-org/example-repo", 7, comments, true, true)
	if err == nil || len(results) != 2 {
		t.Fatalf("results = %+v, error = %v; want a failed thread", results, err)
	}
	if results[0].Reply.Status != ReplyPlanned || results[0].Action != ActionNone || results[0].Error == "" ||
		results[1].Reply.Status != ReplyFailed || results[1].Action != ActionNone || results[1].Error == "" {
		t.Fatalf("dry run did not hold thread actions: %+v", results)
	}
}

func TestEvaluateAllDoesNotReplyWithoutEvaluation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake evaluator requires a POSIX shell")
	}
	opts := EvaluateOptions{Bin: fakeCopilotBin(t, "not a JSON verdict", 0), Prompt: "judge", ReplyReason: true}
	results, _, err := EvaluateAll(context.Background(), nil, replyTestRepo, opts, "example-org/example-repo", 7, []*Comment{{CommentID: 22, RootCommentID: 11}}, true, false)
	if err == nil || len(results) != 1 || results[0].Evaluation != nil || results[0].Reply != nil {
		t.Fatalf("results = %+v, error = %v; want failed evaluation and no reply", results, err)
	}
}

func TestEvaluateAllReplyDisabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake evaluator requires a POSIX shell")
	}
	var requests atomic.Int32
	g := newReplyTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/api/graphql" {
			t.Errorf("unexpected API path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"resolveReviewThread":{"thread":{"id":"THREAD_1","isResolved":true}}}}`)
	}, false)
	opts := EvaluateOptions{Bin: fakeReplyEvaluator(t, EvaluatorCopilot, true, VerdictValid, ""), Prompt: "judge"}
	results, _, err := EvaluateAll(context.Background(), g, replyTestRepo, opts, "example-org/example-repo", 7, []*Comment{{CommentID: 22, ThreadID: "THREAD_1"}}, true, false)
	if err != nil || len(results) != 1 || results[0].Reply != nil || results[0].Action != ActionResolved || requests.Load() != 1 {
		t.Fatalf("disabled reply changed existing behavior: results = %+v, requests = %d, error = %v", results, requests.Load(), err)
	}
}

func TestRenderEvaluationReply(t *testing.T) {
	results := []*EvaluationResult{
		{Comment: &Comment{CommentID: 22, RootCommentID: 11, URL: "https://example.test/comment"}, Evaluation: &Evaluation{Verdict: VerdictValid, Reason: "reason"}, Action: ActionResolved},
		{Comment: &Comment{CommentID: 23, RootCommentID: 11}, Evaluation: &Evaluation{Verdict: VerdictValid, Reason: "reason"}, Action: ActionResolved, Reply: &EvaluationReply{Status: ReplyPosted, CommentID: 301, URL: "https://example.test/reply"}},
	}
	text := render.NewStringRenderer(nil)
	if err := RenderEvaluationResults(&text.Renderer, results); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"reply=posted\n", "reply_id=301\n", "reply_url=https://example.test/reply\n"} {
		if !strings.Contains(text.Stdout.String(), want) {
			t.Errorf("text output %q lacks %q", text.Stdout.String(), want)
		}
	}
	if strings.Count(text.Stdout.String(), "reply=") != 1 {
		t.Errorf("disabled reply appeared in text output: %s", text.Stdout.String())
	}
	exported := render.NewStringRenderer(cmdutil.NewJSONExporter())
	if err := RenderEvaluationResults(&exported.Renderer, results); err != nil {
		t.Fatal(err)
	}
	var data []map[string]json.RawMessage
	if err := json.Unmarshal(exported.Stdout.Bytes(), &data); err != nil {
		t.Fatalf("invalid exported JSON: %v: %s", err, exported.Stdout.String())
	}
	if _, ok := data[0]["reply"]; ok {
		t.Errorf("disabled reply appeared in JSON: %s", exported.Stdout.String())
	}
	var reply EvaluationReply
	if err := json.Unmarshal(data[1]["reply"], &reply); err != nil || reply.Status != ReplyPosted || reply.CommentID != 301 {
		t.Errorf("exported reply = %+v, error = %v", reply, err)
	}
	if strings.Contains(exported.Stdout.String(), "root_comment_id") || strings.Contains(exported.Stdout.String(), "RootCommentID") {
		t.Errorf("internal root ID appeared in JSON: %s", exported.Stdout.String())
	}
}
