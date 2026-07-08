package comments

import "regexp"

// Conservative redaction patterns for common credential shapes. The goal is to
// avoid leaking obvious secrets into the on-disk corpus, not to be a full DLP
// system. Patterns are intentionally narrow to minimize false positives.
var redactPatterns = []*regexp.Regexp{
	// GitHub tokens (classic, fine-grained, server-to-server, user-to-server)
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{20,}`),
	// AWS access key IDs
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// Slack tokens
	regexp.MustCompile(`xox[abposr]-[A-Za-z0-9-]{10,}`),
	// JWT-shaped tokens
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
	// PEM private key headers
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

const redactionPlaceholder = "[REDACTED]"

// Redact replaces matches of redactPatterns with a placeholder. Returns the
// possibly-modified body and a flag indicating whether any replacement
// happened.
func Redact(body string) (string, bool) {
	out := body
	changed := false
	for _, re := range redactPatterns {
		if re.MatchString(out) {
			out = re.ReplaceAllString(out, redactionPlaceholder)
			changed = true
		}
	}
	return out, changed
}
