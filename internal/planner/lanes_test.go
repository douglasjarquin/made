package planner

import "testing"

func TestMatchPath_LiteralPath(t *testing.T) {
	ok, err := matchPath("go.mod", "go.mod")
	if err != nil || !ok {
		t.Fatalf("expected literal match, got ok=%v err=%v", ok, err)
	}
}

func TestMatchPath_DoubleStarMatchesAnyDepth(t *testing.T) {
	cases := map[string]bool{
		"main.go":                              true,
		"internal/planner/plan.go":             true,
		"internal/planner/deep/nested/file.go": true,
		"README.md":                            false,
	}
	for path, want := range cases {
		ok, err := matchPath("**/*.go", path)
		if err != nil {
			t.Fatalf("matchPath(%q): %v", path, err)
		}
		if ok != want {
			t.Fatalf("matchPath(%q) = %v, want %v", path, ok, want)
		}
	}
}

func TestMatchPath_DoubleStarAtRootMatchesTopLevelToo(t *testing.T) {
	ok, err := matchPath("**/*.go", "main.go")
	if err != nil || !ok {
		t.Fatalf("expected ** to match zero leading segments, got ok=%v err=%v", ok, err)
	}
}

func TestMatchPath_SingleSegmentWildcardDoesNotCrossSlash(t *testing.T) {
	ok, err := matchPath("*.go", "internal/planner/plan.go")
	if err != nil {
		t.Fatalf("matchPath: %v", err)
	}
	if ok {
		t.Fatal("expected a single-segment wildcard not to match a nested path")
	}
}

func TestMatchPath_MalformedPatternErrors(t *testing.T) {
	if _, err := matchPath("[unterminated", "anything"); err == nil {
		t.Fatal("expected an error for a malformed pattern")
	}
}
