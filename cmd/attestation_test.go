package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/srz-zumix/gh-review-kit/pkg/attestation"
)

// withFakeEmbed temporarily overrides the embedGitMetadata package variable
// and restores the original after the test completes.
func withFakeEmbed(t *testing.T, fn func(ctx context.Context, opts attestation.EmbedOptions) (*attestation.EmbedResult, error)) {
	t.Helper()
	original := embedGitMetadata
	embedGitMetadata = fn
	t.Cleanup(func() { embedGitMetadata = original })
}

// withReadonly temporarily overrides the isReadonly package variable and
// restores the original after the test completes.
func withReadonly(t *testing.T, value bool) {
	t.Helper()
	original := isReadonly
	isReadonly = func() bool { return value }
	t.Cleanup(func() { isReadonly = original })
}

func TestAttestationCmdRequiresExactlyOneArg(t *testing.T) {
	withReadonly(t, false)
	cmd := NewAttestationCmd()
	cmd.SetArgs([]string{"--output", "out.mp4"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error when no input video is given")
	}

	cmd = NewAttestationCmd()
	cmd.SetArgs([]string{"a.mp4", "b.mp4", "--output", "out.mp4"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error when more than one input video is given")
	}
}

func TestAttestationCmdRequiresOutput(t *testing.T) {
	withReadonly(t, false)
	withFakeEmbed(t, func(ctx context.Context, opts attestation.EmbedOptions) (*attestation.EmbedResult, error) {
		t.Fatal("embedGitMetadata should not be called when --output is missing")
		return nil, nil
	})

	cmd := NewAttestationCmd()
	cmd.SetArgs([]string{"input.mp4"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--output") {
		t.Fatalf("expected a missing --output error, got %v", err)
	}
}

func TestAttestationCmdRejectsReadOnly(t *testing.T) {
	withReadonly(t, true)
	withFakeEmbed(t, func(ctx context.Context, opts attestation.EmbedOptions) (*attestation.EmbedResult, error) {
		t.Fatal("embedGitMetadata should not be called in read-only mode")
		return nil, nil
	})

	cmd := NewAttestationCmd()
	cmd.SetArgs([]string{"input.mp4", "--output", "out.mp4"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected a read-only error, got %v", err)
	}
}

func TestAttestationCmdPropagatesFlags(t *testing.T) {
	withReadonly(t, false)
	var gotOpts attestation.EmbedOptions
	withFakeEmbed(t, func(ctx context.Context, opts attestation.EmbedOptions) (*attestation.EmbedResult, error) {
		gotOpts = opts
		return &attestation.EmbedResult{Output: opts.Output}, nil
	})

	cmd := NewAttestationCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"input.mp4", "--output", "out.mp4", "--repo-dir", "/tmp/repo", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotOpts.Input != "input.mp4" {
		t.Errorf("Input: got %q want %q", gotOpts.Input, "input.mp4")
	}
	if gotOpts.Output != "out.mp4" {
		t.Errorf("Output: got %q want %q", gotOpts.Output, "out.mp4")
	}
	if gotOpts.RepoDir != "/tmp/repo" {
		t.Errorf("RepoDir: got %q want %q", gotOpts.RepoDir, "/tmp/repo")
	}
	if !gotOpts.Force {
		t.Errorf("Force: got false want true")
	}
	if !strings.Contains(buf.String(), "out.mp4") {
		t.Errorf("expected success message to mention output path, got %q", buf.String())
	}
}

func TestAttestationCmdDefaultsRepoDirEmpty(t *testing.T) {
	withReadonly(t, false)
	var gotOpts attestation.EmbedOptions
	withFakeEmbed(t, func(ctx context.Context, opts attestation.EmbedOptions) (*attestation.EmbedResult, error) {
		gotOpts = opts
		return &attestation.EmbedResult{Output: opts.Output}, nil
	})

	cmd := NewAttestationCmd()
	cmd.SetArgs([]string{"input.mp4", "--output", "out.mp4"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotOpts.RepoDir != "" {
		t.Errorf("RepoDir: got %q want empty (default to current directory)", gotOpts.RepoDir)
	}
	if gotOpts.Force {
		t.Errorf("Force: got true want false (default)")
	}
}

func TestAttestationCmdWrapsWorkflowError(t *testing.T) {
	withReadonly(t, false)
	withFakeEmbed(t, func(ctx context.Context, opts attestation.EmbedOptions) (*attestation.EmbedResult, error) {
		return nil, errors.New("ffmpeg exploded")
	})

	cmd := NewAttestationCmd()
	cmd.SetArgs([]string{"input.mp4", "--output", "out.mp4"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "input.mp4") || !strings.Contains(err.Error(), "ffmpeg exploded") {
		t.Fatalf("expected a wrapped error mentioning input path and cause, got %v", err)
	}
}

func TestAttestationCmdReportsWarnings(t *testing.T) {
	withReadonly(t, false)
	withFakeEmbed(t, func(ctx context.Context, opts attestation.EmbedOptions) (*attestation.EmbedResult, error) {
		return &attestation.EmbedResult{
			Output:   opts.Output,
			Warnings: []string{"tag \"git.repository\" was not present in the output container after embedding"},
		}, nil
	})

	cmd := NewAttestationCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"input.mp4", "--output", "out.mp4"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "out.mp4") {
		t.Errorf("expected success message even with warnings, got %q", buf.String())
	}
}
