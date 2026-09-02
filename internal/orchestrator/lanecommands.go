package orchestrator

import (
	"fmt"

	"github.com/douglasjarquin/made/internal/pipeline/test"
	"github.com/douglasjarquin/made/internal/planner"
)

// laneFullCommandsForTest resolves, for the candidate currently in the gate
// worktree, which validation.lanes (config #33 Phase 2) are selected and
// returns their Full commands as test.ExtraCommand values for the Test
// stage to run in addition to commands.test. With no lanes configured it
// returns (nil, nil): zero behavior change from before lanes existed.
func (c *chain) laneFullCommandsForTest() ([]test.ExtraCommand, error) {
	lanes := c.rc.Config.Validation.Lanes
	if len(lanes) == 0 {
		return nil, nil
	}

	changedPaths, err := planner.ChangedPaths(c.ctx, c.rc.Worktree.Path, c.defaultBranch, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("orchestrator: compute changed paths for validation lanes: %w", err)
	}
	decisions, err := planner.SelectLanes(lanes, changedPaths)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: select validation lanes: %w", err)
	}

	var extras []test.ExtraCommand
	for _, decision := range decisions {
		if decision.Action != "run" {
			continue
		}
		lane, ok := lanes[decision.Name]
		if !ok {
			continue
		}
		commands := lane.FullShellCommands()
		for i, cmd := range commands {
			name := decision.Name
			if len(commands) > 1 {
				name = fmt.Sprintf("%s-%d", decision.Name, i+1)
			}
			extras = append(extras, test.ExtraCommand{Name: name, Args: cmd})
		}
	}
	return extras, nil
}
