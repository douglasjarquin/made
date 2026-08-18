package agent

type Kind string

const (
	KindCodex Kind = "codex"
)

func SupportedKinds() []Kind {
	return []Kind{KindCodex}
}

func (k Kind) binaryName() string {
	switch k {
	case KindCodex:
		return "codex"
	default:
		return string(k)
	}
}
