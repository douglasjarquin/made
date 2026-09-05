package agent

import (
	"fmt"
	"strings"
)

// Kind names a reviewer harness Made knows how to spawn in report-only mode.
// Every kind receives the same task text and must return one JSON object
// matching reviewSchema; the harness-specific flags and response envelope live
// in spawn.go.
type Kind string

const (
	KindCodex  Kind = "codex"
	KindClaude Kind = "claude"
	KindCursor Kind = "cursor"
	KindGrok   Kind = "grok"
)

// SupportedKinds lists every harness in stable, documented order.
func SupportedKinds() []Kind {
	return []Kind{KindCodex, KindClaude, KindCursor, KindGrok}
}

// SupportedKindNames renders SupportedKinds for error messages and help text.
func SupportedKindNames() string {
	kinds := SupportedKinds()
	names := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		names = append(names, string(kind))
	}
	return strings.Join(names, ", ")
}

// ParseKind resolves a configured agent name to a supported Kind.
func ParseKind(name string) (Kind, error) {
	for _, kind := range SupportedKinds() {
		if name == string(kind) {
			return kind, nil
		}
	}
	return "", fmt.Errorf("unsupported agent %q; supported agents: %s", name, SupportedKindNames())
}

// BinaryName is the executable name Made looks up on PATH for this kind.
func (k Kind) BinaryName() string {
	switch k {
	case KindCursor:
		return "cursor-agent"
	default:
		return string(k)
	}
}

// stateDirs lists the HOME-relative directories each CLI writes while running a
// single non-interactive turn (auth cache, session logs, telemetry). They stay
// writable inside the Linux containment because the CLIs fail closed without
// them; the candidate's source is still masked read-only.
func (k Kind) stateDirs() []string {
	switch k {
	case KindCodex:
		return []string{".codex"}
	case KindClaude:
		return []string{".claude", ".claude.json", ".config/claude"}
	case KindCursor:
		return []string{".cursor", ".config/cursor-agent", ".local/share/cursor-agent", ".cache/cursor-agent"}
	case KindGrok:
		return []string{".grok"}
	default:
		return nil
	}
}
