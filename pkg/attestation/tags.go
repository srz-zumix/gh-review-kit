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
