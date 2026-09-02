package config

import (
	"fmt"
	"path"
	"strings"
)

// Validation carries additive, opt-in path-aware validation lanes (see
// project issue #33 Phase 2). A repository with no lanes configured keeps
// its full-pipeline behavior exactly as before: internal/planner treats
// that as a single catch-all lane.
type Validation struct {
	Lanes map[string]Lane `yaml:"lanes"`
	// NoReuse disables receipt-based reuse (project issue #33 Phase 3)
	// repository-wide: every selected lane's Full commands always execute,
	// even when a matching receipt exists. Lane selection and enforcement
	// (Phase 2) are unaffected - this only controls whether a successful
	// prior result can stand in for running the commands again.
	NoReuse bool `yaml:"no_reuse"`
}

// Lane groups the local validation relevant to a set of paths. Full
// commands are the proof orchestrator's Test stage runs, in addition to
// commands.test, when this lane is selected. Quick is reserved for a future
// remediation checkpoint made's linear (non-iterative) pipeline does not
// have yet - it is parsed and validated but nothing executes it.
type Lane struct {
	Paths              []string `yaml:"paths"`
	DependsOn          []string `yaml:"depends_on"`
	Quick              []string `yaml:"quick"`
	Full               []string `yaml:"full"`
	RequiredBeforePush bool     `yaml:"required_before_push"`
}

// FullShellCommands tokenizes each Full command the same way
// Config.TestCommand/LintCommand do, skipping empty entries.
func (l Lane) FullShellCommands() [][]string {
	cmds := make([][]string, 0, len(l.Full))
	for _, c := range l.Full {
		if tok := shellCommand(c); tok != nil {
			cmds = append(cmds, tok)
		}
	}
	return cmds
}

func (v Validation) validate() error {
	for name, lane := range v.Lanes {
		if name == "" {
			return fmt.Errorf("config: validation.lanes has an empty lane name")
		}
		if len(lane.Paths) == 0 {
			return fmt.Errorf("config: validation.lanes[%q] must configure at least one path pattern", name)
		}
		for _, pattern := range append(append([]string{}, lane.Paths...), lane.DependsOn...) {
			if err := validateGlobPattern(pattern); err != nil {
				return fmt.Errorf("config: validation.lanes[%q]: %w", name, err)
			}
		}
	}
	return nil
}

// validateGlobPattern rejects a pattern whose non-"**" segments are not
// valid path.Match patterns, without requiring a real file list to test
// against.
func validateGlobPattern(pattern string) error {
	for _, segment := range strings.Split(pattern, "/") {
		if segment == "**" {
			continue
		}
		if _, err := path.Match(segment, ""); err != nil {
			return fmt.Errorf("malformed path pattern %q: %w", pattern, err)
		}
	}
	return nil
}
