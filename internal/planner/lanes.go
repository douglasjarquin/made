package planner

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/douglasjarquin/made/internal/config"
)

type LaneDecision struct {
	Name               string   `json:"name"`
	MatchedPaths       []string `json:"matched_paths,omitempty"`
	Action             string   `json:"action"`
	Reason             string   `json:"reason"`
	RequiredBeforePush bool     `json:"required_before_push"`
}

// selectLanes decides, for each configured lane, whether the changed paths
// select it. With no lanes configured it falls back to Phase 1's single
// catch-all "default" lane, preserving today's full-pipeline behavior.
//
// A path matching no configured lane fails open: every RequiredBeforePush
// lane is forced to "run" regardless of its own path match, since an
// unclassified change cannot be proven safe to skip.
func selectLanes(lanes map[string]config.Lane, changedPaths []string) ([]LaneDecision, error) {
	if len(lanes) == 0 {
		return []LaneDecision{defaultLaneDecision(changedPaths)}, nil
	}

	names := make([]string, 0, len(lanes))
	for name := range lanes {
		names = append(names, name)
	}
	sort.Strings(names)

	matchedByLane := make(map[string][]string, len(lanes))
	matchedAny := make(map[string]bool, len(changedPaths))
	for _, name := range names {
		lane := lanes[name]
		patterns := append(append([]string{}, lane.Paths...), lane.DependsOn...)
		for _, p := range changedPaths {
			matched, err := matchesAny(patterns, p)
			if err != nil {
				return nil, fmt.Errorf("planner: lane %q: %w", name, err)
			}
			if matched {
				matchedByLane[name] = append(matchedByLane[name], p)
				matchedAny[p] = true
			}
		}
	}

	var unmatched []string
	for _, p := range changedPaths {
		if !matchedAny[p] {
			unmatched = append(unmatched, p)
		}
	}
	sort.Strings(unmatched)

	decisions := make([]LaneDecision, 0, len(names))
	for _, name := range names {
		lane := lanes[name]
		matched := matchedByLane[name]
		decisions = append(decisions, laneDecisionFor(name, lane, matched, unmatched))
	}
	return decisions, nil
}

func laneDecisionFor(name string, lane config.Lane, matched, unmatched []string) LaneDecision {
	if len(unmatched) > 0 && lane.RequiredBeforePush {
		return LaneDecision{
			Name:               name,
			MatchedPaths:       matched,
			Action:             "run",
			Reason:             fmt.Sprintf("fail-open: unclassified path(s) %s", strings.Join(unmatched, ", ")),
			RequiredBeforePush: lane.RequiredBeforePush,
		}
	}
	if len(matched) > 0 {
		return LaneDecision{
			Name:               name,
			MatchedPaths:       matched,
			Action:             "run",
			Reason:             fmt.Sprintf("%d path(s) matched", len(matched)),
			RequiredBeforePush: lane.RequiredBeforePush,
		}
	}
	return LaneDecision{
		Name:               name,
		Action:             "skip",
		Reason:             "no matching paths",
		RequiredBeforePush: lane.RequiredBeforePush,
	}
}

func defaultLaneDecision(changedPaths []string) LaneDecision {
	if len(changedPaths) == 0 {
		return LaneDecision{Name: defaultLaneName, Action: "skip", Reason: "no changed paths", RequiredBeforePush: true}
	}
	return LaneDecision{
		Name:               defaultLaneName,
		MatchedPaths:       changedPaths,
		Action:             "run",
		Reason:             fmt.Sprintf("%d path(s) matched lane %q", len(changedPaths), defaultLaneName),
		RequiredBeforePush: true,
	}
}

func matchesAny(patterns []string, filePath string) (bool, error) {
	for _, pattern := range patterns {
		ok, err := matchPath(pattern, filePath)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// matchPath matches filePath against pattern, where "**" as a whole path
// segment matches zero or more path segments (recursive glob) and every
// other segment is matched with path.Match's single-segment semantics
// (which never crosses a "/").
func matchPath(pattern, filePath string) (bool, error) {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(filePath, "/"))
}

func matchSegments(patternSegs, pathSegs []string) (bool, error) {
	if len(patternSegs) == 0 {
		return len(pathSegs) == 0, nil
	}
	if patternSegs[0] == "**" {
		if len(patternSegs) == 1 {
			return true, nil
		}
		for i := 0; i <= len(pathSegs); i++ {
			ok, err := matchSegments(patternSegs[1:], pathSegs[i:])
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	if len(pathSegs) == 0 {
		return false, nil
	}
	ok, err := path.Match(patternSegs[0], pathSegs[0])
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	return matchSegments(patternSegs[1:], pathSegs[1:])
}
