package agent

import (
	"fmt"
	"os"
	stdexec "os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// containedInvocation wraps the reviewer CLI in the platform sandbox.
// writablePaths are the harness's own state directories (session files,
// caches) that must stay writable so the CLI can run at all; everything else
// outside the review worktree is read-only on Linux, and the candidate's
// source is denied on both platforms.
func containedInvocation(binary string, args []string, reviewPath string, protectedPaths, maskPaths, writablePaths []string) (string, []string, error) {
	switch runtime.GOOS {
	case "darwin":
		const sandboxExec = "/usr/bin/sandbox-exec"
		if _, err := os.Stat(sandboxExec); err != nil {
			return "", nil, fmt.Errorf("%s is required for reviewer containment: %w", sandboxExec, err)
		}
		profile := darwinReviewProfile(protectedPaths)
		commandArgs := []string{"-p", profile, binary}
		return sandboxExec, append(commandArgs, args...), nil
	case "linux":
		bwrap, err := stdexec.LookPath("bwrap")
		if err != nil {
			return "", nil, fmt.Errorf("bubblewrap is required for reviewer containment: %w", err)
		}
		return bwrap, bubblewrapReviewArgs(binary, args, reviewPath, protectedPaths, maskPaths, writablePaths), nil
	default:
		return "", nil, fmt.Errorf("reviewer containment is unsupported on %s", runtime.GOOS)
	}
}

func darwinReviewProfile(protectedPaths []string) string {
	var profile strings.Builder
	profile.WriteString("(version 1)\n(allow default)\n")
	for _, path := range protectedPaths {
		quoted := strconv.Quote(path)
		profile.WriteString("(deny file-read* (subpath ")
		profile.WriteString(quoted)
		profile.WriteString("))\n")
		profile.WriteString("(deny file-write* (subpath ")
		profile.WriteString(quoted)
		profile.WriteString("))\n")
	}
	return profile.String()
}

func bubblewrapReviewArgs(binary string, args []string, reviewPath string, protectedPaths, maskPaths, writablePaths []string) []string {
	paths := append([]string(nil), protectedPaths...)
	maskByPath := make(map[string]string, len(protectedPaths))
	for index, path := range protectedPaths {
		maskByPath[path] = maskPaths[index]
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	commandArgs := []string{
		"--die-with-parent",
		"--unshare-pid",
		"--unshare-ipc",
		"--unshare-uts",
		"--ro-bind", "/", "/",
		"--bind", "/tmp", "/tmp",
		"--dev", "/dev",
		"--proc", "/proc",
		"--ro-bind", reviewPath, reviewPath,
	}
	for _, path := range writablePaths {
		commandArgs = append(commandArgs, "--bind", path, path)
	}
	for _, path := range paths {
		commandArgs = append(commandArgs, "--ro-bind", maskByPath[path], path)
	}
	commandArgs = append(commandArgs, "--chdir", reviewPath)
	commandArgs = append(commandArgs, "--", binary)
	return append(commandArgs, args...)
}
