package evidence

import (
	"bytes"
	"regexp"
)

var evidenceSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization:\s*(?:bearer|basic)\s+)[^\s]+`),
	regexp.MustCompile(`(?i)(\b(?:token|api[_-]?key|secret|password|passwd|access[_-]?token|refresh[_-]?token|client[_-]?secret)\b\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;&}]+)`),
	regexp.MustCompile(`(?i)(["']?(?:token|api[_-]?key|secret|password|passwd|access[_-]?token|refresh[_-]?token|client[_-]?secret)["']?\s*:\s*)(?:"[^"]*"|'[^']*'|[^,\s}]+)`),
	regexp.MustCompile(`(?i)(x-api-key:\s*)[^\s]+`),
	regexp.MustCompile(`(?i)(cookie:\s*)[^\r\n]+`),
	regexp.MustCompile(`(?i)(token=|access_token=|refresh_token=|client_secret=)[^&\s]+`),
	regexp.MustCompile(`(?i)(\b(?:database[_-]?url|redis[_-]?url)\s*=\s*)(?:"[^"]*"|'[^']*'|[^\r\n\s]+)`),
	regexp.MustCompile(`(?i)(\b[A-Z][A-Z0-9_]*(?:token|api[_-]?key|secret|password|passwd|database[_-]?url|redis[_-]?url)[A-Z0-9_]*\s*=\s*)(?:"[^"]*"|'[^']*'|[^\r\n\s]+)`),
	regexp.MustCompile(`\b(?:ghp_|github_pat_|sk-|xox[baprs]-)[A-Za-z0-9._-]+`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
}

func Redact(data []byte) []byte {
	redacted := bytes.Clone(data)
	for _, pattern := range evidenceSecretPatterns {
		redacted = pattern.ReplaceAll(redacted, []byte("$1[REDACTED]"))
	}
	return redacted
}

func RedactString(value string) string {
	return string(Redact([]byte(value)))
}
