package agent

import (
	"strconv"
	"strings"
	"testing"
)

func TestExtractStructuredResponsePerHarness(t *testing.T) {
	findings := `{"findings":[]}`
	quoted := strconv.Quote(findings)
	fenced := strconv.Quote("```json\n" + findings + "\n```")
	for _, test := range []struct {
		name   string
		kind   Kind
		stdout string
	}{
		{name: "codex event stream", kind: KindCodex, stdout: `{"type":"thread.started"}` + "\n" + `{"type":"item.completed","item":{"type":"agent_message","text":` + quoted + `}}`},
		{name: "claude structured_output", kind: KindClaude, stdout: `{"type":"result","is_error":false,"result":"ignored","structured_output":` + findings + `}`},
		{name: "claude result text", kind: KindClaude, stdout: `{"type":"result","is_error":false,"result":` + quoted + `}`},
		{name: "cursor fenced result", kind: KindCursor, stdout: `{"type":"result","subtype":"success","is_error":false,"result":` + fenced + `}`},
		{name: "cursor narration then object", kind: KindCursor, stdout: `{"type":"result","subtype":"success","is_error":false,"result":` + strconv.Quote("I'll inspect the commit.Shell was blocked; reading files."+findings) + `}`},
		{name: "grok structuredOutput", kind: KindGrok, stdout: `{"text":"","stopReason":"end_turn","structuredOutput":` + findings + `}`},
		{name: "grok text fallback", kind: KindGrok, stdout: `{"text":` + quoted + `,"stopReason":"end_turn"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, err := extractStructuredResponse(test.kind, []byte(test.stdout))
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			if _, err := strictFindings(response); err != nil {
				t.Fatalf("strictFindings(%s): %v", response, err)
			}
		})
	}
}

func TestExtractStructuredResponseFailsClosedOnHarnessErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		kind   Kind
		stdout string
		want   string
	}{
		{name: "codex turn.failed", kind: KindCodex, stdout: `{"type":"turn.failed","error":{"message":"boom"}}`, want: "failed event"},
		{name: "claude is_error", kind: KindClaude, stdout: `{"type":"result","is_error":true,"result":"Not logged in"}`, want: "Not logged in"},
		{name: "cursor no object", kind: KindCursor, stdout: `{"type":"result","is_error":false,"result":"no findings, all good"}`, want: "did not end with a JSON object"},
		{name: "cursor is_error", kind: KindCursor, stdout: `{"type":"result","is_error":true,"result":"quota"}`, want: "quota"},
		{name: "grok empty", kind: KindGrok, stdout: `{"text":"","stopReason":"max_turns"}`, want: "max_turns"},
		{name: "unknown kind", kind: Kind("copilot"), stdout: `{"x":1}`, want: "unsupported agent"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := extractStructuredResponse(test.kind, []byte(test.stdout))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}
