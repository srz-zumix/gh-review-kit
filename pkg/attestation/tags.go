package attestation

// knownTagOrder lists the known metadata keys this package embeds and reads
// back — the Git provenance tags, in the same order as GitMetadata.Tags,
// followed by CommentTag — so ReadGitMetadata reports them consistently. It is
// a cross-format contract shared by the PNG, JPEG, and video read/embed paths,
// so it lives here (rather than in any single format file) to keep the
// ordering authoritative for all formats.
var knownTagOrder = []string{
	GitTagCommit,
	GitTagBranch,
	GitTagDirty,
	GitTagCommitDate,
	GitTagAuthor,
	GitTagRepository,
	CommentTag,
}

// isKnownTagKey reports whether key is one of the known metadata keys this
// package embeds, so embedders can strip stale provenance tags before writing
// fresh ones and re-embedding stays authoritative.
func isKnownTagKey(key string) bool {
	for _, k := range knownTagOrder {
		if k == key {
			return true
		}
	}
	return false
}

// orderedKnownTags returns the known metadata tags present in found, in
// knownTagOrder, so every format's read path reports them consistently. A
// CommentTag with an empty value is treated as absent, preserving the
// documented contract that the comment tag appears only when a non-empty
// comment was supplied: the write path treats an empty comment as "no comment",
// and the video delete-on-re-embed (-metadata attestation.comment=) can leave
// some containers holding the key with an empty value. Empty values for the
// other (provenance) keys are kept so genuinely malformed metadata is not
// silently hidden.
func orderedKnownTags(found map[string]string) []Tag {
	var tags []Tag
	for _, key := range knownTagOrder {
		value, ok := found[key]
		if !ok || (key == CommentTag && value == "") {
			continue
		}
		tags = append(tags, Tag{Key: key, Value: value})
	}
	return tags
}
