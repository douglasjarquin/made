package config

import (
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/github"
)

const (
	defaultStageTimeout      = 30 * time.Minute
	defaultEvidenceRetention = 4 << 20
	// MaxReviewGuides and MaxReviewGuidePathBytes bound review.guides at
	// config-validation time, before any file is ever read. They are
	// deliberately small: guides are a short, curated index into product
	// knowledge, not a bulk document store (project issue #40).
	MaxReviewGuides         = 20
	MaxReviewGuidePathBytes = 512
	// MaxCursorModelBytes bounds review.executors.cursor.model, a preference
	// only - never a substitution-enforcement boundary (project issue #42).
	MaxCursorModelBytes = 512
)

type Config struct {
	Version                int              `yaml:"version"`
	Document               Document         `yaml:"document"`
	Review                 Review           `yaml:"review"`
	DisableProjectSettings bool             `yaml:"disable_project_settings"`
	NoCI                   bool             `yaml:"no_ci"`
	CI                     CI               `yaml:"ci"`
	Test                   Test             `yaml:"test"`
	Commands               Commands         `yaml:"commands"`
	Agent                  string           `yaml:"agent"`
	Agents                 []string         `yaml:"agents"`
	AllowRepoCommands      bool             `yaml:"allow_repo_commands"`
	Stages                 map[string]Stage `yaml:"stages"`
	Validation             Validation       `yaml:"validation"`
}

type Stage struct {
	Enabled        *bool `yaml:"enabled"`
	TimeoutSeconds *int  `yaml:"timeout_seconds"`
}

func (c Config) StageTimeout(name string) time.Duration {
	stage := c.Stages[name]
	if stage.TimeoutSeconds == nil {
		return defaultStageTimeout
	}
	return time.Duration(*stage.TimeoutSeconds) * time.Second
}

func (c Config) EvidenceRetentionBytes() int {
	if c.Test.Evidence.RetentionBytes == nil {
		return defaultEvidenceRetention
	}
	return *c.Test.Evidence.RetentionBytes
}

func (c Config) StageResult(name string) string {
	if name == "ci" && c.NoCI {
		return "skipped"
	}
	stage, ok := c.Stages[name]
	if ok && stage.Enabled != nil && !*stage.Enabled {
		return "skipped"
	}
	return "pending"
}

func (c Config) StageRequired(name string) bool {
	switch name {
	case "review":
		return c.Review.Required
	case "ci":
		return c.CI.Required && !c.NoCI
	default:
		return true
	}
}

type Document struct {
	Rules []DocumentRule `yaml:"rules"`
}

type DocumentRule struct {
	PathPattern        string `yaml:"path_pattern"`
	RequiredDocPattern string `yaml:"required_doc_pattern"`
}

type Review struct {
	Required  bool            `yaml:"required"`
	Guides    []string        `yaml:"guides"`
	Executors ReviewExecutors `yaml:"executors"`
}

type ReviewExecutors struct {
	Cursor CursorExecutor `yaml:"cursor"`
}

type CursorExecutor struct {
	Model string `yaml:"model"`
}

func (r Review) validate() error {
	if err := validateCursorModel(r.Executors.Cursor.Model); err != nil {
		return err
	}
	if len(r.Guides) > MaxReviewGuides {
		return fmt.Errorf("config: review.guides has %d entries, exceeding the maximum of %d", len(r.Guides), MaxReviewGuides)
	}
	seen := make(map[string]struct{}, len(r.Guides))
	for i, raw := range r.Guides {
		if raw == "" {
			return fmt.Errorf("config: review.guides[%d] must not be empty", i)
		}
		if len(raw) > MaxReviewGuidePathBytes {
			return fmt.Errorf("config: review.guides[%d] %q exceeds %d bytes", i, raw, MaxReviewGuidePathBytes)
		}
		if path.IsAbs(raw) {
			return fmt.Errorf("config: review.guides[%d] %q must be a repository-relative path, not absolute", i, raw)
		}
		cleaned := path.Clean(raw)
		if cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
			return fmt.Errorf("config: review.guides[%d] %q must not escape the repository root", i, raw)
		}
		if _, dup := seen[cleaned]; dup {
			return fmt.Errorf("config: review.guides[%d] %q duplicates an earlier configured guide (normalized: %q)", i, raw, cleaned)
		}
		seen[cleaned] = struct{}{}
	}
	return nil
}

func validateCursorModel(model string) error {
	if model == "" {
		return nil
	}
	if len(model) > MaxCursorModelBytes {
		return fmt.Errorf("config: review.executors.cursor.model exceeds %d bytes", MaxCursorModelBytes)
	}
	if strings.ContainsAny(model, "\r\n") {
		return fmt.Errorf("config: review.executors.cursor.model must be a single line")
	}
	for _, r := range model {
		if unicode.IsControl(r) {
			return fmt.Errorf("config: review.executors.cursor.model must not contain control characters")
		}
	}
	return nil
}

type CI struct {
	Required    bool              `yaml:"required"`
	RerunBudget int               `yaml:"rerun_budget"`
	CheckScope  github.CheckScope `yaml:"check_scope"`
}

type Test struct {
	Evidence Evidence `yaml:"evidence"`
}

type Evidence struct {
	Branch         string `yaml:"branch"`
	StoreInRepo    bool   `yaml:"store_in_repo"`
	Dir            string `yaml:"dir"`
	RetentionBytes *int   `yaml:"retention_bytes"`
}

type Commands struct {
	Test string `yaml:"test"`
	Lint string `yaml:"lint"`
}

func (c Config) TestCommand() []string {
	return shellCommand(c.Commands.Test)
}

func (c Config) LintCommand() []string {
	return shellCommand(c.Commands.Lint)
}

func shellCommand(cmd string) []string {
	if cmd == "" {
		return nil
	}
	return []string{"sh", "-c", cmd}
}

func (c Config) AgentKind() (agent.Kind, error) {
	kind, err := agent.ParseKind(c.Agent)
	if err != nil {
		return "", fmt.Errorf("config: %w", err)
	}
	return kind, nil
}

// AgentIsPinned reports whether Agent names one specific kind (today's
// original behavior: no fallback, no probing). "" and the "auto" sentinel
// both mean autodetect and are not pinned.
func (c Config) AgentIsPinned() bool {
	return c.Agent != "" && c.Agent != "auto"
}

// AgentCandidates returns the ordered autodetect candidate list. It is only
// meaningful when AgentIsPinned() is false; callers must check that first
// and use AgentKind() directly on the pinned path (project issue: agent
// auto-resolve). Precedence: Agents in the configured order when non-empty,
// else agent.SupportedKinds()'s fixed default order - Agents is otherwise a
// parsed-but-unconsumed field (validated per-entry in validateCommon).
func (c Config) AgentCandidates() []agent.Kind {
	if len(c.Agents) == 0 {
		return agent.SupportedKinds()
	}
	kinds := make([]agent.Kind, 0, len(c.Agents))
	for _, configured := range c.Agents {
		// Already validated by validateCommon; ParseKind cannot fail here for
		// a config that passed Validate()/ValidateWithoutReviewAgentRequirement.
		if kind, err := agent.ParseKind(configured); err == nil {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

func (c Config) Validate() error {
	return c.validateCommon()
}

// ValidateWithoutReviewAgentRequirement is kept as a distinct entry point for
// callers (e.g. internal/cursor/doctor.go) that historically needed to
// validate a config satisfiable by an external review result with no
// internal agent at all. Since agent auto-resolve (project: agent
// auto-resolve) made auto/empty Agent always a valid selection mechanism,
// Validate() itself no longer has a stricter agent requirement to relax, so
// both methods currently do the same thing; kept separate so a future
// Validate()-only check doesn't silently start applying to this caller.
func (c Config) ValidateWithoutReviewAgentRequirement() error {
	return c.validateCommon()
}

func (c Config) validateCommon() error {
	if c.AgentIsPinned() {
		if _, err := c.AgentKind(); err != nil {
			return err
		}
	}
	for index, configured := range c.Agents {
		if _, err := agent.ParseKind(configured); err != nil {
			return fmt.Errorf("config: %w at agents[%d]", err, index)
		}
	}
	if err := c.Validation.validate(); err != nil {
		return err
	}
	if err := c.Review.validate(); err != nil {
		return err
	}
	return nil
}
