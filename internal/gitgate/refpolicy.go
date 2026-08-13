package gitgate

import (
	"fmt"
	"strings"
)

const zeroSHA = "0000000000000000000000000000000000000000"

type RefDecision struct {
	Accept    bool
	CreateRun bool
	Message   string
}

func ClassifyRef(ref, defaultBranch, oldSHA, newSHA string) RefDecision {
	branch, isBranch := strings.CutPrefix(ref, "refs/heads/")
	if !isBranch {
		return RefDecision{
			Message: fmt.Sprintf("ref %s is not accepted by this gate - only refs/heads/* branches (except the default) are validated", ref),
		}
	}
	if branch == defaultBranch {
		return RefDecision{
			Message: "pushing the default branch to the gate is not a supported flow",
		}
	}
	if newSHA == zeroSHA {
		return RefDecision{Accept: true}
	}
	return RefDecision{Accept: true, CreateRun: true}
}
