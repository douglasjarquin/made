package main

import (
	"strings"
	"testing"
)

func TestHelpFlagsPrintUsageForTopLevelAndCommandGroups(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		usage string
	}{
		{name: "top-level long", args: []string{"--help"}, usage: "usage: made <command> [args]"},
		{name: "top-level short", args: []string{"-h"}, usage: "usage: made <command> [args]"},
		{name: "run long", args: []string{"run", "--help"}, usage: "usage: made run <submit|status|list|cancel>"},
		{name: "run short", args: []string{"run", "-h"}, usage: "usage: made run <submit|status|list|cancel>"},
		{name: "daemon long", args: []string{"daemon", "--help"}, usage: "usage: made daemon <start|stop|status>"},
		{name: "daemon short", args: []string{"daemon", "-h"}, usage: "usage: made daemon <start|stop|status>"},
		{name: "gate long", args: []string{"gate", "--help"}, usage: "usage: made gate <init|admit-push|notify-push> [args]"},
		{name: "gate short", args: []string{"gate", "-h"}, usage: "usage: made gate <init|admit-push|notify-push> [args]"},
		{name: "config long", args: []string{"config", "--help"}, usage: "usage: made config <path|check|move> [args]"},
		{name: "config short", args: []string{"config", "-h"}, usage: "usage: made config <path|check|move> [args]"},
		{name: "cursor long", args: []string{"cursor", "--help"}, usage: "usage: made cursor <init|sync|check|doctor> [args]"},
		{name: "cursor short", args: []string{"cursor", "-h"}, usage: "usage: made cursor <init|sync|check|doctor> [args]"},
		{name: "review long", args: []string{"review", "--help"}, usage: "usage: made review <decide> [args]"},
		{name: "review short", args: []string{"review", "-h"}, usage: "usage: made review <decide> [args]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runCapture(t, tt.args)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
			}
			if !strings.Contains(string(stdout), tt.usage) {
				t.Fatalf("stdout = %q, want usage %q", stdout, tt.usage)
			}
			if strings.Contains(string(stderr), "unknown command") || strings.Contains(string(stderr), "unknown subcommand") {
				t.Fatalf("stderr = %q, contains unknown-command diagnostic", stderr)
			}
		})
	}
}
