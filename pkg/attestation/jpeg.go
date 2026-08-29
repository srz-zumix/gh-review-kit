package attestation

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

const (
	jpegMarkerSOS = 0xDA // start of scan: entropy-coded data follows, stop parsing markers
	jpegMarkerEOI = 0xD9
	jpegMarkerCOM = 0xFE
)

// jpegEmbedTags returns a copy of a JPEG image with tags added as COM
// (comment) marker segments immediately after the SOI marker.
func jpegEmbedTags(data []byte, tags []Tag) ([]byte, error) {
	if !bytes.HasPrefix(data, jpegSignature[:2]) {
		return nil, fmt.Errorf("not a JPEG file")
	}

	var out bytes.Buffer
	out.Write(data[:2]) // SOI
	for _, tag := range tags {
		seg, err := jpegEncodeCOM([]byte(fmt.Sprintf("%s=%s", tag.Key, tag.Value)))
		if err != nil {
			return nil, fmt.Errorf("failed to embed tag %q: %w", tag.Key, err)
		}
		out.Write(seg)
	}
	out.Write(data[2:])
	return out.Bytes(), nil
}

// jpegEncodeCOM serializes a single COM marker segment carrying payload. The
// COM length field is a 2-byte big-endian value that includes its own two
// bytes, so the payload cannot exceed math.MaxUint16-2 bytes; a larger payload
// would wrap the length and emit an invalid JPEG, so it is rejected instead.
func jpegEncodeCOM(payload []byte) ([]byte, error) {
	if len(payload) > math.MaxUint16-2 {
		return nil, fmt.Errorf("JPEG comment segment payload too large: %d bytes (max %d)", len(payload), math.MaxUint16-2)
	}
	length := make([]byte, 2)
	binary.BigEndian.PutUint16(length, uint16(len(payload)+2))
	buf := []byte{0xFF, jpegMarkerCOM}
	buf = append(buf, length...)
	return append(buf, payload...), nil
}

// jpegReadTags scans marker segments before the start-of-scan marker for
// COM segments matching known "key=value" Git provenance tags.
func jpegReadTags(data []byte) ([]Tag, error) {
	if !bytes.HasPrefix(data, jpegSignature[:2]) {
		return nil, fmt.Errorf("not a JPEG file")
	}

	found := map[string]string{}
	pos := 2
	for pos+2 <= len(data) {
		if data[pos] != 0xFF {
			break // not aligned on a marker
		}
		marker := data[pos+1]
		pos += 2
		if marker == jpegMarkerSOS || marker == jpegMarkerEOI {
			break
		}
		if marker == 0x00 || marker == 0xFF {
			continue // fill byte or padding, no segment
		}
		if marker >= 0xD0 && marker <= 0xD7 || marker == 0x01 {
			continue // RSTn/TEM: standalone markers with no length/payload
		}
		if pos+2 > len(data) {
			break
		}
		segLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if segLen < 2 || pos+segLen > len(data) {
			break
		}
		if marker == jpegMarkerCOM {
			if key, value, ok := strings.Cut(string(data[pos+2:pos+segLen]), "="); ok {
				found[key] = value
			}
		}
		pos += segLen
	}

	var tags []Tag
	for _, key := range gitTagOrder {
		if value, ok := found[key]; ok {
			tags = append(tags, Tag{Key: key, Value: value})
		}
	}
	return tags, nil
}
