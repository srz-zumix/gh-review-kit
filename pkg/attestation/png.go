package attestation

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

type pngChunk struct {
	Type string
	Data []byte
}

// pngParseChunks splits a PNG file into its signature-following chunk
// sequence, without validating CRCs (this package only reads back its own
// tEXt keyword/text pairs, so a corrupt unrelated chunk is not fatal here).
func pngParseChunks(data []byte) ([]pngChunk, error) {
	if !bytes.HasPrefix(data, pngSignature) {
		return nil, fmt.Errorf("not a PNG file")
	}
	var chunks []pngChunk
	pos := len(pngSignature)
	for pos+8 <= len(data) {
		length := binary.BigEndian.Uint32(data[pos : pos+4])
		typ := string(data[pos+4 : pos+8])
		start := pos + 8
		end := start + int(length)
		if length > uint32(len(data)) || end+4 > len(data) {
			return nil, fmt.Errorf("truncated PNG chunk %q", typ)
		}
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

// pngEmbedTags returns a copy of a PNG image with tags added as tEXt chunks
// immediately after the IHDR chunk.
func pngEmbedTags(data []byte, tags []Tag) ([]byte, error) {
	chunks, err := pngParseChunks(data)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	out.Write(pngSignature)
	insertedAfterIHDR := false
	for _, c := range chunks {
		out.Write(pngEncodeChunk(c.Type, c.Data))
		if c.Type == "IHDR" && !insertedAfterIHDR {
			for _, tag := range tags {
				textData := append([]byte(tag.Key+"\x00"), []byte(tag.Value)...)
				out.Write(pngEncodeChunk("tEXt", textData))
			}
			insertedAfterIHDR = true
		}
	}
	if !insertedAfterIHDR {
		return nil, fmt.Errorf("PNG file is missing an IHDR chunk")
	}
	return out.Bytes(), nil
}

// pngReadTags extracts tEXt chunks matching known Git provenance keys.
func pngReadTags(data []byte) ([]Tag, error) {
	chunks, err := pngParseChunks(data)
	if err != nil {
		return nil, err
	}

	found := map[string]string{}
	for _, c := range chunks {
		if c.Type != "tEXt" {
			continue
		}
		idx := bytes.IndexByte(c.Data, 0)
		if idx < 0 {
			continue
		}
		found[string(c.Data[:idx])] = string(c.Data[idx+1:])
	}

	var tags []Tag
	for _, key := range gitTagOrder {
		if value, ok := found[key]; ok {
			tags = append(tags, Tag{Key: key, Value: value})
		}
	}
	return tags, nil
}
