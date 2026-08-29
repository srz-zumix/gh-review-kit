package attestation

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"unicode/utf8"
)

type pngChunk struct {
	Type string
	Data []byte
}

// pngParseChunks splits a PNG file into its signature-following chunk
// sequence, without validating CRCs (this package only reads back its own
// iTXt/tEXt keyword/text pairs, so a corrupt unrelated chunk is not fatal
// here).
func pngParseChunks(data []byte) ([]pngChunk, error) {
	if !bytes.HasPrefix(data, pngSignature) {
		return nil, fmt.Errorf("not a PNG file")
	}
	var chunks []pngChunk
	pos := len(pngSignature)
	for len(data)-pos >= 8 {
		length := binary.BigEndian.Uint32(data[pos : pos+4])
		typ := string(data[pos+4 : pos+8])
		start := pos + 8
		// Validate against the remaining byte count using uint64 before
		// converting length to int, so a malformed length cannot overflow int
		// on 32-bit platforms and cause an out-of-range slice. Each chunk
		// carries its data plus a trailing 4-byte CRC.
		remaining := uint64(len(data) - start)
		if uint64(length)+4 > remaining {
			return nil, fmt.Errorf("truncated PNG chunk %q", typ)
		}
		end := start + int(length)
		chunks = append(chunks, pngChunk{Type: typ, Data: data[start:end]})
		pos = end + 4
		if typ == "IEND" {
			break
		}
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("PNG file has no chunks")
	}
	return chunks, nil
}

// pngEncodeChunk serializes a single PNG chunk, including its length prefix
// and trailing CRC32 over the type and data.
func pngEncodeChunk(typ string, data []byte) []byte {
	buf := make([]byte, 0, 12+len(data))
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(data)))
	buf = append(buf, length...)
	buf = append(buf, []byte(typ)...)
	buf = append(buf, data...)
	crc := crc32.ChecksumIEEE(append([]byte(typ), data...))
	crcBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBytes, crc)
	return append(buf, crcBytes...)
}

// pngEncodeITXt builds the data field of an uncompressed iTXt chunk carrying a
// UTF-8 text value. Unlike tEXt (Latin-1 by spec), iTXt stores UTF-8, so Git
// branch and author names that contain non-Latin-1 characters are preserved
// faithfully. The layout is: keyword + NUL + compressionFlag(0) +
// compressionMethod(0) + languageTag + NUL + translatedKeyword + NUL + text.
// The language tag and translated keyword are left empty here.
func pngEncodeITXt(key, value string) []byte {
	buf := make([]byte, 0, len(key)+5+len(value))
	buf = append(buf, key...)
	buf = append(buf, 0x00) // keyword null separator
	buf = append(buf, 0x00) // compression flag: uncompressed
	buf = append(buf, 0x00) // compression method
	buf = append(buf, 0x00) // empty language tag + its null separator
	buf = append(buf, 0x00) // empty translated keyword + its null separator
	buf = append(buf, value...)
	return buf
}

// pngParseITXt decodes the data field of an iTXt chunk, returning its keyword
// and UTF-8 text. It only accepts uncompressed text (compression flag 0), as
// this package never emits compressed iTXt, and rejects malformed chunks or
// text that is not valid UTF-8 or contains an embedded NUL.
func pngParseITXt(data []byte) (string, string, bool) {
	sep := bytes.IndexByte(data, 0)
	if sep <= 0 || sep > 79 {
		return "", "", false
	}
	key := string(data[:sep])
	rest := data[sep+1:]
	if len(rest) < 2 {
		return "", "", false
	}
	compFlag, compMethod := rest[0], rest[1]
	rest = rest[2:]
	if compFlag != 0 || compMethod != 0 {
		return "", "", false // compressed or unknown method: skip
	}
	langSep := bytes.IndexByte(rest, 0)
	if langSep < 0 {
		return "", "", false
	}
	rest = rest[langSep+1:]
	transSep := bytes.IndexByte(rest, 0)
	if transSep < 0 {
		return "", "", false
	}
	text := rest[transSep+1:]
	if !utf8.Valid(text) || bytes.IndexByte(text, 0) >= 0 {
		return "", "", false
	}
	return key, string(text), true
}

// pngParseTEXt decodes the data field of a legacy tEXt chunk into its keyword
// and text (split on the first NUL).
func pngParseTEXt(data []byte) (string, string, bool) {
	idx := bytes.IndexByte(data, 0)
	if idx < 0 {
		return "", "", false
	}
	return string(data[:idx]), string(data[idx+1:]), true
}

// pngTextChunkKeyword returns the keyword of a tEXt or iTXt chunk, used to
// detect existing provenance chunks that should be replaced when re-embedding.
func pngTextChunkKeyword(c pngChunk) (string, bool) {
	if c.Type != "tEXt" && c.Type != "iTXt" {
		return "", false
	}
	sep := bytes.IndexByte(c.Data, 0)
	if sep <= 0 {
		return "", false
	}
	return string(c.Data[:sep]), true
}

// pngEmbedTags returns a copy of a PNG image with tags added as iTXt chunks
// immediately after the IHDR chunk. Any existing provenance text chunks are
// dropped first, so re-embedding replaces prior metadata rather than leaving
// stale values that would win under last-value-wins reads. Tag values must be
// valid UTF-8.
func pngEmbedTags(data []byte, tags []Tag) ([]byte, error) {
	for _, tag := range tags {
		if !utf8.ValidString(tag.Value) {
			return nil, fmt.Errorf("PNG tag %q value is not valid UTF-8", tag.Key)
		}
	}

	chunks, err := pngParseChunks(data)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	out.Write(pngSignature)
	insertedAfterIHDR := false
	for _, c := range chunks {
		if key, ok := pngTextChunkKeyword(c); ok && isKnownTagKey(key) {
			continue // replace existing provenance metadata
		}
		out.Write(pngEncodeChunk(c.Type, c.Data))
		if c.Type == "IHDR" && !insertedAfterIHDR {
			for _, tag := range tags {
				out.Write(pngEncodeChunk("iTXt", pngEncodeITXt(tag.Key, tag.Value)))
			}
			insertedAfterIHDR = true
		}
	}
	if !insertedAfterIHDR {
		return nil, fmt.Errorf("PNG file is missing an IHDR chunk")
	}
	return out.Bytes(), nil
}

// pngReadTags extracts Git provenance metadata from iTXt chunks (preferred) and
// legacy tEXt chunks. iTXt takes precedence over tEXt for the same key
// regardless of chunk order, so UTF-8 values are not shadowed by an older
// Latin-1 tEXt entry.
func pngReadTags(data []byte) ([]Tag, error) {
	chunks, err := pngParseChunks(data)
	if err != nil {
		return nil, err
	}

	found := map[string]string{}
	fromITXt := map[string]bool{}
	for _, c := range chunks {
		switch c.Type {
		case "iTXt":
			key, value, ok := pngParseITXt(c.Data)
			if !ok {
				continue
			}
			found[key] = value
			fromITXt[key] = true
		case "tEXt":
			key, value, ok := pngParseTEXt(c.Data)
			if !ok || fromITXt[key] {
				continue
			}
			found[key] = value
		}
	}

	return orderedKnownTags(found), nil
}
