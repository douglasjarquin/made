package agent

type Kind string

const (
	KindClaude Kind = "claude"
	KindCodex  Kind = "codex"
)

func (k Kind) binaryName() string {
	switch k {
	case KindClaude:
		return "claude"
	case KindCodex:
		return "codex"
	default:
		return string(k)
	}
}
