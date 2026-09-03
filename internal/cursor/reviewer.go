package cursor

import (
	"errors"
	"fmt"
	"strings"

	"github.com/douglasjarquin/made/internal/agent"
)

const (
	ReviewerName        = "made-reviewer"
	ReviewerDescription = "Independently review the exact committed candidate for Made verification."
)

var ErrReviewerModelRequired = errors.New("cursor: review.executors.cursor.model must be configured to generate the reviewer")

// ReviewerMarkdown renders .cursor/agents/made-reviewer.md. model is the
// exact, opaque value from review.executors.cursor.model - copied verbatim
// into frontmatter, never "inherit" and never substituted. guides are the
// trusted, repository-relative review.guides paths (project issue #40); only
// their paths are referenced here, never their content.
func ReviewerMarkdown(model string, guides []string) (string, error) {
	if strings.TrimSpace(model) == "" {
		return "", ErrReviewerModelRequired
	}

	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", ReviewerName)
	fmt.Fprintf(&b, "description: %s\n", ReviewerDescription)
	fmt.Fprintf(&b, "model: %s\n", model)
	b.WriteString("readonly: true\n")
	b.WriteString("is_background: false\n")
	b.WriteString("---\n\n")

	b.WriteString("You are Made's independent external Review executor.\n\n")
	b.WriteString("Review only the exact base-to-input candidate in the prepared Made request.\n")
	b.WriteString("Do not edit files, create commits, push, open pull requests, or change\n")
	b.WriteString("repository state.\n\n")

	b.WriteString(agent.ReviewGuideInstruction + "\n")
	if len(guides) > 0 {
		b.WriteString("\nConfigured Review guides (read exact trusted bytes named in the prepared\n")
		b.WriteString("request; never copy their content into .cursor/):\n")
		for _, g := range guides {
			fmt.Fprintf(&b, "- %s\n", g)
		}
	}
	b.WriteString("\n")

	b.WriteString("Return only the schema-valid Made external Review result: one JSON document\n")
	b.WriteString("with schema_version, review_contract_version, executor, reviewer,\n")
	b.WriteString("requested_model, actual_model (optional), base_sha, input_sha, policy_hash,\n")
	b.WriteString("review_contract_hash, findings, and guides_consulted (when guides were\n")
	b.WriteString("configured) - exactly as `made verify complete` validates. Never return prose;\n")
	b.WriteString("Made does not convert free-form text into findings.\n\n")

	b.WriteString("<!-- " + GeneratedMarker + " -->\n")
	return b.String(), nil
}
