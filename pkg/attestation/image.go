package attestation

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

// imageFormat identifies a still-image container whose Git provenance
// metadata can be embedded and read natively in Go, without ffmpeg/ffprobe.
type imageFormat int

const (
	imageFormatUnknown imageFormat = iota
	imageFormatPNG
	imageFormatJPEG
)

var (
	pngSignature  = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	jpegSignature = []byte{0xFF, 0xD8, 0xFF}
)

// detectImageFormat identifies data's format from its magic bytes, returning
// imageFormatUnknown for anything not recognized as PNG or JPEG.
func detectImageFormat(data []byte) imageFormat {
	if bytes.HasPrefix(data, pngSignature) {
		return imageFormatPNG
	}
	if bytes.HasPrefix(data, jpegSignature) {
		return imageFormatJPEG
	}
	return imageFormatUnknown
}

// peekImageFormat identifies path's image format by reading only its leading
// bytes, so large non-image (e.g. video) files are not read into memory.
func peekImageFormat(path string) (imageFormat, error) {
	f, err := os.Open(path)
	if err != nil {
		return imageFormatUnknown, err
	}
	defer f.Close()

	buf := make([]byte, len(pngSignature))
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return imageFormatUnknown, err
	}
	return detectImageFormat(buf[:n]), nil
}

// embedImageTags returns a copy of data with tags embedded as native
// metadata for the given image format (PNG tEXt chunks or JPEG COM segments).
func embedImageTags(format imageFormat, data []byte, tags []Tag) ([]byte, error) {
	switch format {
	case imageFormatPNG:
		return pngEmbedTags(data, tags)
	case imageFormatJPEG:
		return jpegEmbedTags(data, tags)
	default:
		return nil, fmt.Errorf("unsupported image format")
	}
}

// readImageTags reads previously embedded Git provenance tags from data,
// returning only the entries matching gitTagOrder.
func readImageTags(format imageFormat, data []byte) ([]Tag, error) {
	switch format {
	case imageFormatPNG:
		return pngReadTags(data)
	case imageFormatJPEG:
		return jpegReadTags(data)
	default:
		return nil, fmt.Errorf("unsupported image format")
	}
}

// warningsForMismatch compares expected tags against a map of tags actually
// found after embedding, returning human-readable warnings for anything
// missing or altered. A failure to run or parse ffprobe's output is a fatal
// error, handled separately by the caller.
func warningsForMismatch(tags []Tag, got map[string]string) []string {
	var warnings []string
	for _, tag := range tags {
		value, ok := got[tag.Key]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("tag %q was not present in the output file after embedding", tag.Key))
			continue
		}
		if value != tag.Value {
			warnings = append(warnings, fmt.Sprintf("tag %q was rewritten by the output file: got %q want %q", tag.Key, value, tag.Value))
		}
	}
	return warnings
}
