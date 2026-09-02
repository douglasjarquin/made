package config

import (
	"fmt"
	"path"
	"strings"
	"time"

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
	Required bool     `yaml:"required"`
	Guides   []string `yaml:"guides"`
}

func (r Review) validate() error {
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
	switch c.Agent {
	case string(agent.KindCodex):
		return agent.KindCodex, nil
	default:
		return "", fmt.Errorf("config: unsupported agent %q; supported agents: %q", c.Agent, agent.KindCodex)
	}
}

func (c Config) Validate() error {
	if err := c.validateReviewRequiresAgent(); err != nil {
		return err
	}
	return c.validateCommon()
}

// ValidateWithoutReviewAgentRequirement runs every check Validate does
// except that required Review implies a configured internal agent. Made's
// managed-validation mode can satisfy required Review with a caller-supplied
// external result instead of an agent, so it enforces that invariant itself,
// once it knows which Review source a run will actually use.
func (c Config) ValidateWithoutReviewAgentRequirement() error {
	return c.validateCommon()
}

func (c Config) validateReviewRequiresAgent() error {
	if c.Review.Required && c.Agent == "" {
		return fmt.Errorf("config: review requires agent %q", agent.KindCodex)
	}
	return nil
}

func (c Config) validateCommon() error {
	if c.Agent != "" {
		if _, err := c.AgentKind(); err != nil {
			return err
		}
	}
	for index, configured := range c.Agents {
		if configured != string(agent.KindCodex) {
			return fmt.Errorf("config: unsupported agent %q at agents[%d]; supported agents: %q", configured, index, agent.KindCodex)
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
