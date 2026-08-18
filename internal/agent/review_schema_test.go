package agent

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestReviewSchemaRequiresEveryFindingProperty(t *testing.T) {
	var schema struct {
		Properties struct {
			Findings struct {
				Items struct {
					Properties map[string]json.RawMessage `json:"properties"`
					Required   []string                   `json:"required"`
				} `json:"items"`
			} `json:"findings"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(reviewSchema), &schema); err != nil {
		t.Fatalf("review schema is not JSON: %v", err)
	}
	if len(schema.Properties.Findings.Items.Properties) != len(schema.Properties.Findings.Items.Required) {
		t.Fatalf("strict output schema must require every finding property: properties=%v required=%v", schema.Properties.Findings.Items.Properties, schema.Properties.Findings.Items.Required)
	}
	for _, property := range []string{"kind", "description", "patch", "paths"} {
		if _, ok := schema.Properties.Findings.Items.Properties[property]; !ok {
			t.Fatalf("review schema missing finding property %q", property)
		}
		if !slices.Contains(schema.Properties.Findings.Items.Required, property) {
			t.Fatalf("review schema does not require finding property %q", property)
		}
	}
}

func TestStrictFindingsRejectsMissingRequiredNullableProperties(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
	}{
		{name: "missing patch", data: `{"findings":[{"kind":"ask-user","description":"needs a decision","paths":null}]}`},
		{name: "missing paths", data: `{"findings":[{"kind":"ask-user","description":"needs a decision","patch":null}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := strictFindings([]byte(test.data)); err == nil {
				t.Fatal("expected missing required property to fail closed")
			}
		})
	}
}

func TestStrictFindingsAcceptsExplicitNullPropertiesForNonAutoFixes(t *testing.T) {
	findings, err := strictFindings([]byte(`{"findings":[{"kind":"ask-user","description":"needs a decision","patch":null,"paths":null}]}`))
	if err != nil {
		t.Fatalf("strictFindings: %v", err)
	}
	if len(findings.Findings) != 1 || findings.Findings[0].Patch != "" || findings.Findings[0].Paths != nil {
		t.Fatalf("unexpected finding: %+v", findings.Findings)
	}
}
