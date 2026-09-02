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
}

// Lane groups the local validation relevant to a set of paths. Quick
// commands give fast feedback during remediation; Full commands are the
// proof required before Push when RequiredBeforePush is set. Neither list
// runs anywhere yet - Phase 2's execution wiring is a separate change.
type Lane struct {
	Paths              []string `yaml:"paths"`
	DependsOn          []string `yaml:"depends_on"`
	Quick              []string `yaml:"quick"`
	Full               []string `yaml:"full"`
	RequiredBeforePush bool     `yaml:"required_before_push"`
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
