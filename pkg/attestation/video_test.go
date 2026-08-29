package attestation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner is a commandRunner test double that records ffmpeg invocations
// and returns a scripted ffprobe response.
type fakeRunner struct {
	lookPathErr map[string]error

	ffmpegArgs [][]string
	ffmpegErr  error
	// ffmpegWrite, when set, is written to the ffmpeg output path (last arg)
	// to simulate ffmpeg producing a file.
	ffmpegWrite []byte

	ffprobeArgs [][]string
	ffprobeOut  []byte
	ffprobeErr  error
}

func (f *fakeRunner) LookPath(name string) (string, error) {
	if err, ok := f.lookPathErr[name]; ok {
		return "", err
	}
	return "/usr/bin/" + name, nil
}

func (f *fakeRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	switch {
	case strings.HasSuffix(name, "ffmpeg"):
		f.ffmpegArgs = append(f.ffmpegArgs, args)
		if f.ffmpegErr != nil {
			return nil, f.ffmpegErr
		}
		if f.ffmpegWrite != nil {
			out := args[len(args)-1]
			if err := os.WriteFile(out, f.ffmpegWrite, 0o644); err != nil {
				return nil, err
			}
		}
		return nil, nil
	case strings.HasSuffix(name, "ffprobe"):
		f.ffprobeArgs = append(f.ffprobeArgs, args)
		return f.ffprobeOut, f.ffprobeErr
	default:
		return nil, fmt.Errorf("unexpected command: %s", name)
	}
}

func probeJSON(tags map[string]string) []byte {
	out, err := json.Marshal(ffprobeFormat{Format: struct {
		Tags map[string]string `json:"tags"`
	}{Tags: tags}})
	if err != nil {
		panic(err)
	}
	return out
}

func sampleTags() []Tag {
	return []Tag{
		{Key: GitTagCommit, Value: "abc123"},
		{Key: GitTagBranch, Value: "main"},
		{Key: GitTagDirty, Value: "false"},
		{Key: GitTagCommitDate, Value: "2024-01-01T00:00:00Z"},
		{Key: GitTagAuthor, Value: "Jane Doe <jane@example.com>"},
		{Key: GitTagRepository, Value: "github.com/owner/repo"},
	}
}

func tagsMap(tags []Tag) map[string]string {
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[t.Key] = t.Value
	}
	return m
}

func TestFfmpegArgsStreamCopyAndMetadata(t *testing.T) {
	args := ffmpegArgs("in.mp4", "out.mp4", sampleTags())

	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-y", "-i in.mp4",
		"-map 0", "-map_metadata 0", "-map_chapters 0",
		"-c copy",
		"-metadata git.commit=abc123",
		"-metadata git.branch=main",
		"-metadata git.dirty=false",
		"-metadata git.commit_date=2024-01-01T00:00:00Z",
		"-metadata git.author=Jane Doe <jane@example.com>",
		"-metadata git.repository=github.com/owner/repo",
		"-movflags use_metadata_tags",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("ffmpeg args missing %q:\n%s", want, joined)
		}
	}
	if args[len(args)-1] != "out.mp4" {
		t.Errorf("last arg (output) = %q, want out.mp4", args[len(args)-1])
	}
}

func TestFfmpegArgsNoMovFlagsForNonMovContainer(t *testing.T) {
	args := ffmpegArgs("in.mkv", "out.mkv", sampleTags())
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "use_metadata_tags") {
		t.Errorf("did not expect use_metadata_tags for .mkv output:\n%s", joined)
	}
}

func TestFfmpegArgsDeletesInheritedCommentWhenNone(t *testing.T) {
	// With no comment tag, an inherited attestation.comment must be deleted so
	// -map_metadata 0 does not carry a stale comment across a re-embed.
	args := ffmpegArgs("in.mp4", "out.mp4", sampleTags())
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-metadata attestation.comment=") {
		t.Errorf("expected empty comment delete arg, got:\n%s", joined)
	}
}

func TestFfmpegArgsKeepsProvidedComment(t *testing.T) {
	tags := append(sampleTags(), Tag{Key: CommentTag, Value: "my note"})
	args := ffmpegArgs("in.mp4", "out.mp4", tags)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-metadata attestation.comment=my note") {
		t.Errorf("expected provided comment to be set, got:\n%s", joined)
	}
	// No empty delete arg when a comment is provided.
	if strings.Contains(joined, "-metadata attestation.comment= ") || strings.HasSuffix(joined, "-metadata attestation.comment=") {
		t.Errorf("did not expect an empty comment delete when a comment is provided:\n%s", joined)
	}
}

func TestVerifyTagsAllMatch(t *testing.T) {
	tags := sampleTags()
	runner := &fakeRunner{ffprobeOut: probeJSON(tagsMap(tags))}

	warnings, err := verifyTags(context.Background(), runner, "ffprobe", "out.mp4", tags)
	if err != nil {
		t.Fatalf("verifyTags: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
}

func TestVerifyTagsMissingIsWarning(t *testing.T) {
	tags := sampleTags()
	got := tagsMap(tags)
	delete(got, GitTagRepository)
	runner := &fakeRunner{ffprobeOut: probeJSON(got)}

	warnings, err := verifyTags(context.Background(), runner, "ffprobe", "out.webm", tags)
	if err != nil {
		t.Fatalf("verifyTags: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], GitTagRepository) {
		t.Fatalf("expected one warning about %s, got %v", GitTagRepository, warnings)
	}
}

func TestVerifyTagsMismatchIsWarning(t *testing.T) {
	tags := sampleTags()
	got := tagsMap(tags)
	got[GitTagBranch] = "mangled"
	runner := &fakeRunner{ffprobeOut: probeJSON(got)}

	warnings, err := verifyTags(context.Background(), runner, "ffprobe", "out.mp4", tags)
	if err != nil {
		t.Fatalf("verifyTags: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], GitTagBranch) {
		t.Fatalf("expected one warning about %s, got %v", GitTagBranch, warnings)
	}
}

func TestVerifyTagsProbeFailureIsFatal(t *testing.T) {
	runner := &fakeRunner{ffprobeErr: errors.New("boom")}
	if _, err := verifyTags(context.Background(), runner, "ffprobe", "out.mp4", sampleTags()); err == nil {
		t.Fatal("expected an error when ffprobe fails")
	}
}

func TestVerifyTagsInvalidJSONIsFatal(t *testing.T) {
	runner := &fakeRunner{ffprobeOut: []byte("not json")}
	if _, err := verifyTags(context.Background(), runner, "ffprobe", "out.mp4", sampleTags()); err == nil {
		t.Fatal("expected an error for invalid ffprobe JSON")
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %q: %v", path, err)
	}
}

func TestValidateInputOutputMissingInput(t *testing.T) {
	dir := t.TempDir()
	err := validateInputOutput(filepath.Join(dir, "missing.mp4"), filepath.Join(dir, "out.mp4"), false)
	if err == nil {
		t.Fatal("expected an error for a missing input file")
	}
}

func TestValidateInputOutputSameFile(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mp4")
	writeFile(t, in, "data")
	err := validateInputOutput(in, in, false)
	if err == nil {
		t.Fatal("expected an error when input and output are the same file")
	}
}

func TestValidateInputOutputExistingWithoutForce(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mp4")
	out := filepath.Join(dir, "out.mp4")
	writeFile(t, in, "data")
	writeFile(t, out, "existing")

	err := validateInputOutput(in, out, false)
	if err == nil {
		t.Fatal("expected an error when output exists and --force is not set")
	}
}

func TestValidateInputOutputExistingWithForce(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mp4")
	out := filepath.Join(dir, "out.mp4")
	writeFile(t, in, "data")
	writeFile(t, out, "existing")

	if err := validateInputOutput(in, out, true); err != nil {
		t.Fatalf("validateInputOutput with force: %v", err)
	}
}

func TestValidateInputOutputMissingOutputDir(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mp4")
	writeFile(t, in, "data")
	out := filepath.Join(dir, "missing-dir", "out.mp4")

	if err := validateInputOutput(in, out, false); err == nil {
		t.Fatal("expected an error when the output directory does not exist")
	}
}

func TestValidateInputOutputRejectsDirectoryOutput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mp4")
	writeFile(t, in, "data")
	out := filepath.Join(dir, "out.mp4")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if err := validateInputOutput(in, out, true); err == nil {
		t.Fatal("expected an error when output exists and is a directory, even with --force")
	}
}

func TestPromoteOutputNoExistingFile(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mp4")
	tmp := filepath.Join(dir, "tmp.mp4")
	out := filepath.Join(dir, "out.mp4")
	writeFile(t, in, "input-content")
	writeFile(t, tmp, "content")

	if err := promoteOutput(in, tmp, out, false); err != nil {
		t.Fatalf("promoteOutput: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "content" {
		t.Fatalf("output content = %q, want %q", data, "content")
	}
	// promoteOutput publishes without consuming tmp; the staging file is the
	// caller's responsibility to clean up. It must still contain the content.
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("expected tmp file to remain for the caller to clean up: %v", err)
	}
}

func TestPublishNoReplaceCreatesOutput(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "tmp.mp4")
	out := filepath.Join(dir, "out.mp4")
	writeFile(t, tmp, "content")

	if err := publishNoReplace(tmp, out); err != nil {
		t.Fatalf("publishNoReplace: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "content" {
		t.Fatalf("output content = %q, want %q", data, "content")
	}
}

func TestPublishNoReplaceRefusesExistingOutput(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "tmp.mp4")
	out := filepath.Join(dir, "out.mp4")
	writeFile(t, tmp, "new-content")
	writeFile(t, out, "existing-content")

	if err := publishNoReplace(tmp, out); err == nil {
		t.Fatal("expected an error when output already exists (no-replace publish)")
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "existing-content" {
		t.Fatalf("expected the existing output to be preserved, got %q", data)
	}
}

func TestPromoteOutputForceReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mp4")
	tmp := filepath.Join(dir, "tmp.mp4")
	out := filepath.Join(dir, "out.mp4")
	writeFile(t, in, "input-content")
	writeFile(t, tmp, "new-content")
	writeFile(t, out, "old-content")

	if err := promoteOutput(in, tmp, out, true); err != nil {
		t.Fatalf("promoteOutput: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "new-content" {
		t.Fatalf("output content = %q, want %q", data, "new-content")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	// The input file plus the final output file should remain.
	if len(entries) != 2 {
		t.Fatalf("expected only the input and final output files to remain, got %v", entries)
	}
}

func TestPromoteOutputForceDoesNotClobberFixedBackupName(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mp4")
	tmp := filepath.Join(dir, "tmp.mp4")
	out := filepath.Join(dir, "out.mp4")
	// A file that the previous fixed-name backup scheme would have destroyed.
	unrelatedBak := out + ".attestation-bak"
	writeFile(t, in, "input-content")
	writeFile(t, tmp, "new-content")
	writeFile(t, out, "old-content")
	writeFile(t, unrelatedBak, "precious")

	if err := promoteOutput(in, tmp, out, true); err != nil {
		t.Fatalf("promoteOutput: %v", err)
	}
	data, err := os.ReadFile(unrelatedBak)
	if err != nil {
		t.Fatalf("expected the unrelated backup file to be preserved: %v", err)
	}
	if string(data) != "precious" {
		t.Fatalf("unrelated backup content = %q, want %q", data, "precious")
	}
}

func TestPromoteOutputRejectsDirectoryOutput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mp4")
	tmp := filepath.Join(dir, "tmp.mp4")
	out := filepath.Join(dir, "out.mp4")
	writeFile(t, in, "input-content")
	writeFile(t, tmp, "new-content")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if err := promoteOutput(in, tmp, out, true); err == nil {
		t.Fatal("expected an error when the output path is an existing directory")
	}
	if info, err := os.Stat(out); err != nil || !info.IsDir() {
		t.Fatalf("expected the output directory to be left intact, stat=%v err=%v", info, err)
	}
}

func TestPromoteOutputRejectsSameFileAlias(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mp4")
	tmp := filepath.Join(dir, "tmp.mp4")
	alias := filepath.Join(dir, "alias.mp4")
	writeFile(t, in, "input-content")
	writeFile(t, tmp, "new-content")
	if err := os.Link(in, alias); err != nil {
		t.Skipf("hard links not supported on this filesystem: %v", err)
	}

	if err := promoteOutput(in, tmp, alias, true); err == nil {
		t.Fatal("expected an error when output is a hard link alias of input")
	}
}

func TestPromoteOutputRollsBackOnFailedRename(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mp4")
	tmp := filepath.Join(dir, "tmp.mp4")
	out := filepath.Join(dir, "sub", "out.mp4")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, in, "input-content")
	writeFile(t, tmp, "new-content")
	writeFile(t, out, "old-content")

	// Remove the tmp file's directory read/write ability is hard to break
	// portably; instead simulate the failure by removing tmp before rename,
	// which forces os.Rename(tmp, out) to fail after the backup was made.
	if err := os.Remove(tmp); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	err := promoteOutput(in, tmp, out, true)
	if err == nil {
		t.Fatal("expected an error when the tmp file no longer exists")
	}
	data, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(data) != "old-content" {
		t.Fatalf("expected rollback to restore old-content, got %q", data)
	}
}

func TestEmbedGitMetadataEndToEndWithFakeRunner(t *testing.T) {
	dir := initRepo(t)
	in := filepath.Join(dir, "input.mp4")
	writeFile(t, in, "fake-video-bytes")
	out := filepath.Join(dir, "output.mp4")

	wantSHA := runGit(t, dir, "rev-parse", "HEAD")

	runner := &fakeRunner{ffmpegWrite: []byte("fake-video-bytes")}
	// The ffprobe stub must return whatever tags will actually be computed;
	// build them by first collecting metadata the same way EmbedGitMetadata does.
	meta, err := CollectGitMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("CollectGitMetadata: %v", err)
	}
	if meta.Commit != wantSHA {
		t.Fatalf("sanity check failed: got %q want %q", meta.Commit, wantSHA)
	}
	runner.ffprobeOut = probeJSON(tagsMap(meta.Tags()))

	result, err := embedGitMetadata(context.Background(), EmbedOptions{
		Input:   in,
		Output:  out,
		RepoDir: dir,
	}, runner)
	if err != nil {
		t.Fatalf("embedGitMetadata: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", result.Warnings)
	}
	if result.Output != out {
		t.Fatalf("Output = %q, want %q", result.Output, out)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected output file to exist: %v", err)
	}
	if len(runner.ffmpegArgs) != 1 {
		t.Fatalf("expected exactly one ffmpeg invocation, got %d", len(runner.ffmpegArgs))
	}

	// No leftover temp files should remain in the output directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "attestation") {
			t.Fatalf("unexpected leftover temp entry: %s", e.Name())
		}
	}
}

func TestEmbedGitMetadataWithCommentEndToEndWithFakeRunner(t *testing.T) {
	dir := initRepo(t)
	in := filepath.Join(dir, "input.mp4")
	writeFile(t, in, "fake-video-bytes")
	out := filepath.Join(dir, "output.mp4")

	runner := &fakeRunner{ffmpegWrite: []byte("fake-video-bytes")}
	meta, err := CollectGitMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("CollectGitMetadata: %v", err)
	}
	wantTags := append(meta.Tags(), Tag{Key: CommentTag, Value: "pre-release build"})
	runner.ffprobeOut = probeJSON(tagsMap(wantTags))

	result, err := embedGitMetadata(context.Background(), EmbedOptions{
		Input:   in,
		Output:  out,
		RepoDir: dir,
		Comment: "pre-release build",
	}, runner)
	if err != nil {
		t.Fatalf("embedGitMetadata: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", result.Warnings)
	}

	last := result.Tags[len(result.Tags)-1]
	if last.Key != CommentTag || last.Value != "pre-release build" {
		t.Fatalf("expected trailing comment tag, got %v", last)
	}

	if len(runner.ffmpegArgs) != 1 {
		t.Fatalf("expected exactly one ffmpeg invocation, got %d", len(runner.ffmpegArgs))
	}
	if !strings.Contains(strings.Join(runner.ffmpegArgs[0], " "), "-metadata attestation.comment=pre-release build") {
		t.Fatalf("ffmpeg args missing comment metadata:\n%v", runner.ffmpegArgs[0])
	}

	// Read back through the same stub and confirm knownTagOrder placement
	// (comment last) via an exact ordered comparison.
	readResult, err := readGitMetadata(context.Background(), ReadOptions{Input: out}, runner)
	if err != nil {
		t.Fatalf("readGitMetadata: %v", err)
	}
	if len(readResult.Tags) != len(result.Tags) {
		t.Fatalf("readGitMetadata tags = %v, want %v", readResult.Tags, result.Tags)
	}
	for i, tag := range result.Tags {
		if readResult.Tags[i] != tag {
			t.Fatalf("tag[%d] = %v, want %v", i, readResult.Tags[i], tag)
		}
	}
}

func TestEmbedGitMetadataFfmpegFailureLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "input.mp4")
	writeFile(t, in, "fake-video-bytes")
	out := filepath.Join(dir, "output.mp4")

	runner := &fakeRunner{ffmpegErr: errors.New("ffmpeg exploded")}
	_, err := embedGitMetadata(context.Background(), EmbedOptions{
		Input:   in,
		Output:  out,
		RepoDir: initRepo(t),
	}, runner)
	if err == nil {
		t.Fatal("expected an error when ffmpeg fails")
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("output file should not have been created, stat err = %v", statErr)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("ReadDir: %v", readErr)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "attestation") {
			t.Fatalf("unexpected leftover temp entry: %s", e.Name())
		}
	}
}

func TestEmbedGitMetadataMissingFfmpeg(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "input.mp4")
	writeFile(t, in, "data")
	out := filepath.Join(dir, "output.mp4")

	runner := &fakeRunner{lookPathErr: map[string]error{"ffmpeg": errors.New("not found")}}
	_, err := embedGitMetadata(context.Background(), EmbedOptions{Input: in, Output: out, RepoDir: dir}, runner)
	if err == nil || !strings.Contains(err.Error(), "ffmpeg") {
		t.Fatalf("expected an ffmpeg-not-found error, got %v", err)
	}
}

func TestEmbedGitMetadataMissingFfprobe(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "input.mp4")
	writeFile(t, in, "data")
	out := filepath.Join(dir, "output.mp4")

	runner := &fakeRunner{lookPathErr: map[string]error{"ffprobe": errors.New("not found")}}
	_, err := embedGitMetadata(context.Background(), EmbedOptions{Input: in, Output: out, RepoDir: dir}, runner)
	if err == nil || !strings.Contains(err.Error(), "ffprobe") {
		t.Fatalf("expected an ffprobe-not-found error, got %v", err)
	}
}

func TestEmbedGitMetadataRequiresInputAndOutput(t *testing.T) {
	if _, err := embedGitMetadata(context.Background(), EmbedOptions{Output: "out.mp4"}, &fakeRunner{}); err == nil {
		t.Fatal("expected an error when input is empty")
	}
	if _, err := embedGitMetadata(context.Background(), EmbedOptions{Input: "in.mp4"}, &fakeRunner{}); err == nil {
		t.Fatal("expected an error when output is empty")
	}
}
