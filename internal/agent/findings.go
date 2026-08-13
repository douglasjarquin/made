package agent

type FindingKind string

const (
	FindingAutoFixable FindingKind = "auto-fixable"
	FindingAskUser     FindingKind = "ask-user"
	FindingBlocking    FindingKind = "blocking"
)

type Finding struct {
	Kind        FindingKind `json:"kind"`
	Description string      `json:"description"`
	Patch       string      `json:"patch,omitempty"`
}

type Findings struct {
	Findings []Finding `json:"findings"`
}
