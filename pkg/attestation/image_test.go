package attestation

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func samplePNG(t *testing.T) []byte {
	t.Helper()
	// Minimal 1x1 PNG: signature + IHDR + IDAT + IEND.
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE,
		0x00, 0x00, 0x00, 0x0C, 'I', 'D', 'A', 'T',
		0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00, 0x00, 0x03, 0x01, 0x01, 0x00,
		0x18, 0xDD, 0x8D, 0xB0,
		0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xAE, 0x42, 0x60, 0x82,
	}
}

func sampleJPEG(t *testing.T) []byte {
	t.Helper()
	// Minimal JPEG: SOI, a bare comment marker, EOI. The COM length (0x0007)
	// counts the two length bytes plus the 5-byte "hello" payload.
	return []byte{0xFF, 0xD8, 0xFF, 0xFE, 0x00, 0x07, 'h', 'e', 'l', 'l', 'o', 0xFF, 0xD9}
}

func TestDetectImageFormat(t *testing.T) {
	if got := detectImageFormat(samplePNG(t)); got != imageFormatPNG {
		t.Fatalf("detectImageFormat(png) = %v, want imageFormatPNG", got)
	}
	if got := detectImageFormat(sampleJPEG(t)); got != imageFormatJPEG {
		t.Fatalf("detectImageFormat(jpeg) = %v, want imageFormatJPEG", got)
	}
	if got := detectImageFormat([]byte("not an image")); got != imageFormatUnknown {
		t.Fatalf("detectImageFormat(text) = %v, want imageFormatUnknown", got)
	}
}

func TestPNGEmbedAndReadTagsRoundTrip(t *testing.T) {
	tags := []Tag{{Key: GitTagCommit, Value: "abc123"}, {Key: GitTagBranch, Value: "main"}}

	out, err := pngEmbedTags(samplePNG(t), tags)
	if err != nil {
		t.Fatalf("pngEmbedTags: %v", err)
	}
	if detectImageFormat(out) != imageFormatPNG {
		t.Fatal("expected output to still be a valid PNG")
	}

	got, err := pngReadTags(out)
	if err != nil {
		t.Fatalf("pngReadTags: %v", err)
	}
	if len(got) != len(tags) {
		t.Fatalf("pngReadTags = %v, want %v", got, tags)
	}
	for i, tag := range tags {
		if got[i] != tag {
			t.Fatalf("tag[%d] = %v, want %v", i, got[i], tag)
		}
	}
}

func TestPNGEmbedUTF8ViaITXt(t *testing.T) {
	// A branch name with non-Latin-1 (Japanese) characters must survive a
	// round-trip and be stored in an iTXt chunk, not a Latin-1 tEXt chunk.
	tags := []Tag{{Key: GitTagBranch, Value: "feature/日本語"}}

	out, err := pngEmbedTags(samplePNG(t), tags)
	if err != nil {
		t.Fatalf("pngEmbedTags: %v", err)
	}
	if !bytes.Contains(out, []byte("iTXt")) {
		t.Fatal("expected UTF-8 value to be stored in an iTXt chunk")
	}
	if bytes.Contains(out, []byte("tEXt")) {
		t.Fatal("expected no legacy tEXt chunk to be written")
	}

	got, err := pngReadTags(out)
	if err != nil {
		t.Fatalf("pngReadTags: %v", err)
	}
	if len(got) != 1 || got[0] != tags[0] {
		t.Fatalf("pngReadTags = %v, want %v", got, tags)
	}
}

func TestPNGReEmbedReplacesProvenance(t *testing.T) {
	first, err := pngEmbedTags(samplePNG(t), []Tag{{Key: GitTagBranch, Value: "old"}})
	if err != nil {
		t.Fatalf("pngEmbedTags(first): %v", err)
	}
	second, err := pngEmbedTags(first, []Tag{{Key: GitTagBranch, Value: "new"}})
	if err != nil {
		t.Fatalf("pngEmbedTags(second): %v", err)
	}

	got, err := pngReadTags(second)
	if err != nil {
		t.Fatalf("pngReadTags: %v", err)
	}
	want := []Tag{{Key: GitTagBranch, Value: "new"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("pngReadTags = %v, want %v", got, want)
	}
}

func TestJPEGReEmbedReplacesProvenance(t *testing.T) {
	first, err := jpegEmbedTags(sampleJPEG(t), []Tag{{Key: GitTagBranch, Value: "old"}})
	if err != nil {
		t.Fatalf("jpegEmbedTags(first): %v", err)
	}
	second, err := jpegEmbedTags(first, []Tag{{Key: GitTagBranch, Value: "new"}})
	if err != nil {
		t.Fatalf("jpegEmbedTags(second): %v", err)
	}

	got, err := jpegReadTags(second)
	if err != nil {
		t.Fatalf("jpegReadTags: %v", err)
	}
	want := []Tag{{Key: GitTagBranch, Value: "new"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("jpegReadTags = %v, want %v", got, want)
	}
}

func TestJPEGEmbedAndReadTagsRoundTrip(t *testing.T) {
	tags := []Tag{{Key: GitTagCommit, Value: "abc123"}, {Key: GitTagDirty, Value: "true"}}

	out, err := jpegEmbedTags(sampleJPEG(t), tags)
	if err != nil {
		t.Fatalf("jpegEmbedTags: %v", err)
	}
	if detectImageFormat(out) != imageFormatJPEG {
		t.Fatal("expected output to still be a valid JPEG")
	}

	got, err := jpegReadTags(out)
	if err != nil {
		t.Fatalf("jpegReadTags: %v", err)
	}
	if len(got) != len(tags) {
		t.Fatalf("jpegReadTags = %v, want %v", got, tags)
	}
	for i, tag := range tags {
		if got[i] != tag {
			t.Fatalf("tag[%d] = %v, want %v", i, got[i], tag)
		}
	}
}

func TestEmbedAndReadGitMetadataJPEGEndToEnd(t *testing.T) {
	dir := initRepo(t)
	in := filepath.Join(dir, "input.jpg")
	if err := os.WriteFile(in, decodableJPEG(t), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	out := filepath.Join(dir, "output.jpg")

	ctx := context.Background()
	embedResult, err := EmbedGitMetadata(ctx, EmbedOptions{Input: in, Output: out, RepoDir: dir})
	if err != nil {
		t.Fatalf("EmbedGitMetadata: %v", err)
	}
	if len(embedResult.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", embedResult.Warnings)
	}

	// The promoted output must exist at the requested path and remain a
	// decodable JPEG after embedding.
	outBytes, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile(out): %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(outBytes)); err != nil {
		t.Fatalf("output is not a decodable JPEG: %v", err)
	}

	readResult, err := ReadGitMetadata(ctx, ReadOptions{Input: out})
	if err != nil {
		t.Fatalf("ReadGitMetadata: %v", err)
	}
	if len(readResult.Tags) != len(embedResult.Tags) {
		t.Fatalf("ReadGitMetadata tags = %v, want %v", readResult.Tags, embedResult.Tags)
	}
	for i, tag := range embedResult.Tags {
		if readResult.Tags[i] != tag {
			t.Fatalf("tag[%d] = %v, want %v", i, readResult.Tags[i], tag)
		}
	}
}

// decodableJPEG returns a genuine 1x1 JPEG produced by the standard library, so
// tests can assert that embedding preserves a decodable image (unlike the
// minimal marker-only sampleJPEG fixture).
func decodableJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestEmbedAndReadGitMetadataPNGEndToEnd(t *testing.T) {
	dir := initRepo(t)
	in := filepath.Join(dir, "input.png")
	if err := os.WriteFile(in, samplePNG(t), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	out := filepath.Join(dir, "output.png")

	ctx := context.Background()
	embedResult, err := EmbedGitMetadata(ctx, EmbedOptions{Input: in, Output: out, RepoDir: dir})
	if err != nil {
		t.Fatalf("EmbedGitMetadata: %v", err)
	}
	if len(embedResult.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", embedResult.Warnings)
	}

	readResult, err := ReadGitMetadata(ctx, ReadOptions{Input: out})
	if err != nil {
		t.Fatalf("ReadGitMetadata: %v", err)
	}
	if len(readResult.Tags) != len(embedResult.Tags) {
		t.Fatalf("ReadGitMetadata tags = %v, want %v", readResult.Tags, embedResult.Tags)
	}
}

func TestEmbedAndReadGitMetadataPNGWithComment(t *testing.T) {
	dir := initRepo(t)
	in := filepath.Join(dir, "input.png")
	if err := os.WriteFile(in, samplePNG(t), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	out := filepath.Join(dir, "output.png")

	ctx := context.Background()
	embedResult, err := EmbedGitMetadata(ctx, EmbedOptions{Input: in, Output: out, RepoDir: dir, Comment: "pre-release build"})
	if err != nil {
		t.Fatalf("EmbedGitMetadata: %v", err)
	}
	if len(embedResult.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", embedResult.Warnings)
	}
	last := embedResult.Tags[len(embedResult.Tags)-1]
	if last.Key != CommentTag || last.Value != "pre-release build" {
		t.Fatalf("expected trailing comment tag, got %v", last)
	}

	readResult, err := ReadGitMetadata(ctx, ReadOptions{Input: out})
	if err != nil {
		t.Fatalf("ReadGitMetadata: %v", err)
	}
	if len(readResult.Tags) != len(embedResult.Tags) {
		t.Fatalf("ReadGitMetadata tags = %v, want %v", readResult.Tags, embedResult.Tags)
	}
	found := false
	for _, tag := range readResult.Tags {
		if tag.Key == CommentTag {
			found = true
			if tag.Value != "pre-release build" {
				t.Fatalf("comment tag value = %q, want %q", tag.Value, "pre-release build")
			}
		}
	}
	if !found {
		t.Fatal("expected comment tag to be read back")
	}
}

func TestEmbedAndReadGitMetadataJPEGWithComment(t *testing.T) {
	dir := initRepo(t)
	in := filepath.Join(dir, "input.jpg")
	if err := os.WriteFile(in, decodableJPEG(t), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	out := filepath.Join(dir, "output.jpg")

	ctx := context.Background()
	embedResult, err := EmbedGitMetadata(ctx, EmbedOptions{Input: in, Output: out, RepoDir: dir, Comment: "pre-release build"})
	if err != nil {
		t.Fatalf("EmbedGitMetadata: %v", err)
	}
	if len(embedResult.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", embedResult.Warnings)
	}
	last := embedResult.Tags[len(embedResult.Tags)-1]
	if last.Key != CommentTag || last.Value != "pre-release build" {
		t.Fatalf("expected trailing comment tag, got %v", last)
	}

	outBytes, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile(out): %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(outBytes)); err != nil {
		t.Fatalf("output is not a decodable JPEG: %v", err)
	}

	readResult, err := ReadGitMetadata(ctx, ReadOptions{Input: out})
	if err != nil {
		t.Fatalf("ReadGitMetadata: %v", err)
	}
	if len(readResult.Tags) != len(embedResult.Tags) {
		t.Fatalf("ReadGitMetadata tags = %v, want %v", readResult.Tags, embedResult.Tags)
	}
	for i, tag := range embedResult.Tags {
		if readResult.Tags[i] != tag {
			t.Fatalf("tag[%d] = %v, want %v", i, readResult.Tags[i], tag)
		}
	}
}

func TestPNGReEmbedRemovesComment(t *testing.T) {
	withComment, err := pngEmbedTags(samplePNG(t), []Tag{
		{Key: GitTagBranch, Value: "main"},
		{Key: CommentTag, Value: "old"},
	})
	if err != nil {
		t.Fatalf("pngEmbedTags(withComment): %v", err)
	}
	// Re-embedding without a comment must strip the previously embedded one.
	withoutComment, err := pngEmbedTags(withComment, []Tag{{Key: GitTagBranch, Value: "main"}})
	if err != nil {
		t.Fatalf("pngEmbedTags(withoutComment): %v", err)
	}

	got, err := pngReadTags(withoutComment)
	if err != nil {
		t.Fatalf("pngReadTags: %v", err)
	}
	for _, tag := range got {
		if tag.Key == CommentTag {
			t.Fatalf("expected comment tag to be removed, got %v", got)
		}
	}
}

func TestJPEGReEmbedRemovesComment(t *testing.T) {
	withComment, err := jpegEmbedTags(sampleJPEG(t), []Tag{
		{Key: GitTagBranch, Value: "main"},
		{Key: CommentTag, Value: "old"},
	})
	if err != nil {
		t.Fatalf("jpegEmbedTags(withComment): %v", err)
	}
	withoutComment, err := jpegEmbedTags(withComment, []Tag{{Key: GitTagBranch, Value: "main"}})
	if err != nil {
		t.Fatalf("jpegEmbedTags(withoutComment): %v", err)
	}

	got, err := jpegReadTags(withoutComment)
	if err != nil {
		t.Fatalf("jpegReadTags: %v", err)
	}
	for _, tag := range got {
		if tag.Key == CommentTag {
			t.Fatalf("expected comment tag to be removed, got %v", got)
		}
	}
}
