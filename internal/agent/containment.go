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

func containedInvocation(binary string, args []string, reviewPath string, protectedPaths []string) (string, []string, error) {
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
		return bwrap, bubblewrapReviewArgs(binary, args, reviewPath, protectedPaths), nil
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

func bubblewrapReviewArgs(binary string, args []string, reviewPath string, protectedPaths []string) []string {
	paths := append([]string(nil), protectedPaths...)
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
	for _, path := range paths {
		commandArgs = append(commandArgs, "--perms", "0555", "--tmpfs", path)
	}
	commandArgs = append(commandArgs, "--chdir", reviewPath)
	commandArgs = append(commandArgs, "--", binary)
	return append(commandArgs, args...)
}
