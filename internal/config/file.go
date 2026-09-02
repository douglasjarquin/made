package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/douglasjarquin/made/internal/github"
	"gopkg.in/yaml.v3"
)

const (
	defaultCIRerunBudget   = 2
	maxStageTimeoutSeconds = 2 * 60 * 60
	maxEvidenceRetention   = 64 << 20
	maxConfigBytes         = 1 << 20
)

var validStageNames = map[string]struct{}{
	"intent": {}, "rebase": {}, "review": {}, "test": {}, "document": {},
	"lint": {}, "push": {}, "pr": {}, "ci": {},
}

func LoadEffectiveConfig(trustedPath, pushedPath string) (Config, error) {
	trusted, trustedExists, err := loadConfigFile(trustedPath)
	if err != nil {
		return Config{}, fmt.Errorf("config: trusted copy at %q could not be read: %w", trustedPath, err)
	}

	pushed, pushedExists, err := loadConfigFile(pushedPath)
	if err != nil {
		return Config{}, fmt.Errorf("config: pushed copy at %q could not be read: %w", pushedPath, err)
	}

	if !trustedExists && pushedExists {
		effective := pushed
		if effective.CI.RerunBudget == 0 {
			effective.CI.RerunBudget = defaultCIRerunBudget
		}
		if effective.CI.CheckScope == "" {
			effective.CI.CheckScope = github.CheckScopeRequired
		}
		if err := effective.Validate(); err != nil {
			return Config{}, fmt.Errorf("config: validate effective configuration: %w", err)
		}
		return effective, nil
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
		Validation:             trusted.Validation,
	}
	effective.Test.Evidence = trusted.Test.Evidence

	if effective.CI.RerunBudget == 0 {
		effective.CI.RerunBudget = defaultCIRerunBudget
	}
	if effective.CI.CheckScope == "" {
		effective.CI.CheckScope = github.CheckScopeRequired
	}

	if trustedExists && trusted.AllowRepoCommands {
		effective.Commands = pushed.Commands
		effective.Agent = pushed.Agent
		effective.Agents = pushed.Agents
	} else {
		effective.Commands = trusted.Commands
		effective.Agent = trusted.Agent
		effective.Agents = trusted.Agents
	}
	if err := effective.Validate(); err != nil {
		return Config{}, fmt.Errorf("config: validate effective configuration: %w", err)
	}

	return effective, nil
}

func loadConfigFile(path string) (cfg Config, exists bool, err error) {
	if path == "" {
		return Config{}, false, nil
	}

	data, exists, err := readConfigBytes(path, nil)
	if err != nil {
		return Config{}, exists, err
	}
	if !exists {
		return Config{}, false, nil
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, true, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, true, fmt.Errorf("configuration must contain one YAML document")
		}
		return Config{}, true, err
	}
	if filepath.Base(path) == ".made.yml" || strings.HasSuffix(filepath.Base(path), ".made.yml") {
		if cfg.Version != 1 {
			return Config{}, true, fmt.Errorf("versioned .made.yml requires version: 1, got %d", cfg.Version)
		}
		for name := range cfg.Stages {
			if _, ok := validStageNames[name]; !ok {
				return Config{}, true, fmt.Errorf("versioned .made.yml has unknown stage %q", name)
			}
			stage := cfg.Stages[name]
			if stage.TimeoutSeconds != nil && (*stage.TimeoutSeconds <= 0 || *stage.TimeoutSeconds > maxStageTimeoutSeconds) {
				return Config{}, true, fmt.Errorf("versioned .made.yml stage %q timeout_seconds must be between 1 and %d", name, maxStageTimeoutSeconds)
			}
		}
		if retention := cfg.Test.Evidence.RetentionBytes; retention != nil && (*retention <= 0 || *retention > maxEvidenceRetention) {
			return Config{}, true, fmt.Errorf("versioned .made.yml test.evidence.retention_bytes must be between 1 and %d", maxEvidenceRetention)
		}
		if cfg.CI.CheckScope != "" && !cfg.CI.CheckScope.Valid() {
			return Config{}, true, fmt.Errorf("versioned .made.yml ci.check_scope must be %q or %q, got %q", github.CheckScopeRequired, github.CheckScopeAll, cfg.CI.CheckScope)
		}
		if !cfg.hasConfiguredValue() {
			return Config{}, true, fmt.Errorf("versioned .made.yml must configure at least one non-version field")
		}
		return cfg, true, nil
	}
	return cfg, true, nil
}

func readConfigBytes(path string, beforeRead func()) ([]byte, bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, true, err
	}
	if !info.Mode().IsRegular() {
		return nil, true, fmt.Errorf("config: %s is not a regular file", path)
	}
	if info.Size() > maxConfigBytes {
		return nil, true, fmt.Errorf("config: %s exceeds %d bytes", path, maxConfigBytes)
	}
	if beforeRead != nil {
		beforeRead()
	}
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return nil, true, err
	}
	if len(data) > maxConfigBytes {
		return nil, true, fmt.Errorf("config: %s exceeds %d bytes", path, maxConfigBytes)
	}
	return data, true, nil
}

func (c Config) hasConfiguredValue() bool {
	return len(c.Document.Rules) > 0 || c.Review.Required || c.DisableProjectSettings || c.NoCI ||
		c.CI.Required || c.CI.RerunBudget != 0 || c.CI.CheckScope != "" || len(c.Test.Evidence.Branch) > 0 || c.Test.Evidence.RetentionBytes != nil ||
		c.Test.Evidence.StoreInRepo || len(c.Test.Evidence.Dir) > 0 || len(c.Commands.Test) > 0 ||
		len(c.Commands.Lint) > 0 || len(c.Agent) > 0 || len(c.Agents) > 0 || c.AllowRepoCommands ||
		len(c.Stages) > 0 || len(c.Validation.Lanes) > 0
}
