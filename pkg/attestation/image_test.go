package attestation

import (
	"context"
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
