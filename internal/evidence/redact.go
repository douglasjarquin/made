package evidence

import (
	"bytes"
	"regexp"
)

var evidenceSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)[A-Za-z0-9._-]+`),
	regexp.MustCompile(`\b(?:ghp_|github_pat_|sk-)[A-Za-z0-9_-]+`),
	regexp.MustCompile(`(?i)(token=)[^&\s]+`),
}

func Redact(data []byte) []byte {
	redacted := bytes.Clone(data)
	for _, pattern := range evidenceSecretPatterns {
		redacted = pattern.ReplaceAll(redacted, []byte("$1[REDACTED]"))
	}
	return redacted
}
