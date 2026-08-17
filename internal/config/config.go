package config

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/douglasjarquin/made/internal/agent"
	"gopkg.in/yaml.v3"
)

const defaultCIRerunBudget = 2

type Config struct {
	Document               Document `yaml:"document"`
	Review                 Review   `yaml:"review"`
	DisableProjectSettings bool     `yaml:"disable_project_settings"`
	NoCI                   bool     `yaml:"no_ci"`
	CI                     CI       `yaml:"ci"`
	Test                   Test     `yaml:"test"`
	Commands               Commands `yaml:"commands"`
	Agent                  string   `yaml:"agent"`
	Agents                 []string `yaml:"agents"`
	AllowRepoCommands      bool     `yaml:"allow_repo_commands"`
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
		Document:               trusted.Document,
		Review:                 trusted.Review,
		DisableProjectSettings: trusted.DisableProjectSettings,
		NoCI:                   trusted.NoCI,
		CI:                     trusted.CI,
		AllowRepoCommands:      trusted.AllowRepoCommands,
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

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, true, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Config{}, true, fmt.Errorf("multiple YAML documents are not supported")
		}
		return Config{}, true, err
	}

	return cfg, true, nil
}
