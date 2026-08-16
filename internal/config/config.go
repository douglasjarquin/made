package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/douglasjarquin/made/internal/agent"
	"gopkg.in/yaml.v3"
)

const defaultCIRerunBudget = 2

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
}

type Stage struct {
	Enabled *bool `yaml:"enabled"`
}

func (c Config) StageResult(name string) string {
	stage, ok := c.Stages[name]
	if ok && stage.Enabled != nil && !*stage.Enabled {
		return "skipped"
	}
	return "pending"
}

type Document struct {
	Rules []DocumentRule `yaml:"rules"`
}

type DocumentRule struct {
	PathPattern        string `yaml:"path_pattern"`
	RequiredDocPattern string `yaml:"required_doc_pattern"`
}

type Review struct {
	Required bool `yaml:"required"`
}

type CI struct {
	Required    bool `yaml:"required"`
	RerunBudget int  `yaml:"rerun_budget"`
}

type Test struct {
	Evidence Evidence `yaml:"evidence"`
}

type Evidence struct {
	Branch      string `yaml:"branch"`
	StoreInRepo bool   `yaml:"store_in_repo"`
	Dir         string `yaml:"dir"`
}

type Commands struct {
	Test string `yaml:"test"`
	Lint string `yaml:"lint"`
}

// LoadEffectiveConfig resolves a gate run's effective configuration from a
// trusted source (the default-branch copy) and a pushed source (the branch
// being validated). trustedPath or pushedPath may be "" to indicate that
// source has no config at all.
func LoadEffectiveConfig(trustedPath, pushedPath string) (Config, error) {
	trusted, trustedExists, err := loadConfigFile(trustedPath)
	if err != nil {
		return Config{}, fmt.Errorf("config: trusted copy at %q could not be read: %w", trustedPath, err)
	}

	pushed, _, err := loadConfigFile(pushedPath)
	if err != nil {
		return Config{}, fmt.Errorf("config: pushed copy at %q could not be read: %w", pushedPath, err)
	}

	effective := Config{
		Version:                trusted.Version,
		Document:               trusted.Document,
		Review:                 trusted.Review,
		DisableProjectSettings: trusted.DisableProjectSettings,
		NoCI:                   trusted.NoCI,
		CI:                     trusted.CI,
		AllowRepoCommands:      trusted.AllowRepoCommands,
		Stages:                 trusted.Stages,
	}
	effective.Test.Evidence = trusted.Test.Evidence

	if effective.CI.RerunBudget == 0 {
		effective.CI.RerunBudget = defaultCIRerunBudget
	}

	// Trust boundary: Commands/Agent/Agents execute inside the gate worktree,
	// so a pushed branch must never control them unless the trusted copy
	// itself opted in via allow_repo_commands. Absence of a trusted copy
	// (trustedExists == false) always resolves these to zero-value; it must
	// never fall through to the pushed copy.
	if trustedExists && trusted.AllowRepoCommands {
		effective.Commands = pushed.Commands
		effective.Agent = pushed.Agent
		effective.Agents = pushed.Agents
	} else {
		effective.Commands = trusted.Commands
		effective.Agent = trusted.Agent
		effective.Agents = trusted.Agents
	}

	return effective, nil
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
	case string(agent.KindClaude):
		return agent.KindClaude, nil
	case string(agent.KindCodex):
		return agent.KindCodex, nil
	default:
		return "", fmt.Errorf("config: invalid agent %q: must be %q or %q", c.Agent, agent.KindClaude, agent.KindCodex)
	}
}

func loadConfigFile(path string) (cfg Config, exists bool, err error) {
	if path == "" {
		return Config{}, false, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, false, nil
		}
		return Config{}, false, err
	}

	if filepath.Base(path) == ".made.yml" {
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(&cfg); err != nil {
			return Config{}, true, err
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			if err == nil {
				return Config{}, true, fmt.Errorf("versioned .made.yml must contain one document")
			}
			return Config{}, true, err
		}
		if cfg.Version != 1 {
			return Config{}, true, fmt.Errorf("versioned .made.yml requires version: 1, got %d", cfg.Version)
		}
		if !cfg.hasConfiguredValue() {
			return Config{}, true, fmt.Errorf("versioned .made.yml must configure at least one non-version field")
		}
		return cfg, true, nil
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, true, err
	}

	return cfg, true, nil
}

func (c Config) hasConfiguredValue() bool {
	return len(c.Document.Rules) > 0 || c.Review.Required || c.DisableProjectSettings || c.NoCI ||
		c.CI.Required || c.CI.RerunBudget != 0 || len(c.Test.Evidence.Branch) > 0 ||
		c.Test.Evidence.StoreInRepo || len(c.Test.Evidence.Dir) > 0 || len(c.Commands.Test) > 0 ||
		len(c.Commands.Lint) > 0 || len(c.Agent) > 0 || len(c.Agents) > 0 || c.AllowRepoCommands ||
		len(c.Stages) > 0
}
