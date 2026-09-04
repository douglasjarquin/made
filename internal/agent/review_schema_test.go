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
	// Required properties must all appear in properties.
	for _, req := range schema.Properties.Findings.Items.Required {
		if _, ok := schema.Properties.Findings.Items.Properties[req]; !ok {
			t.Fatalf("required finding property %q is not in properties", req)
		}
	}
	for _, property := range []string{"kind", "description", "patch", "paths"} {
		if _, ok := schema.Properties.Findings.Items.Properties[property]; !ok {
			t.Fatalf("review schema missing finding property %q", property)
		}
		if !slices.Contains(schema.Properties.Findings.Items.Required, property) {
			t.Fatalf("review schema does not require finding property %q", property)
		}
	}
	// Strict structured-output modes (Codex, Claude) reject any property that
	// is not also listed in required, so every property must be required and
	// the semantically optional ones must accept null instead.
	for name := range schema.Properties.Findings.Items.Properties {
		if !slices.Contains(schema.Properties.Findings.Items.Required, name) {
			t.Fatalf("review schema property %q is not required; strict schema modes reject that", name)
		}
	}
	for _, optional := range []string{"code", "class", "symbol"} {
		raw, ok := schema.Properties.Findings.Items.Properties[optional]
		if !ok {
			t.Fatalf("review schema missing optional finding property %q", optional)
		}
		var prop struct {
			Type []string `json:"type"`
		}
		if err := json.Unmarshal(raw, &prop); err != nil || !slices.Contains(prop.Type, "null") {
			t.Fatalf("optional finding property %q must be nullable, got %s", optional, raw)
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

func TestFindingMarshalIncludesRequiredNullableProperties(t *testing.T) {
	data, err := json.Marshal(Findings{Findings: []Finding{{Kind: FindingAskUser, Description: "needs a decision"}}})
	if err != nil {
		t.Fatalf("marshal findings: %v", err)
	}
	var finding map[string]json.RawMessage
	var payload struct {
		Findings []map[string]json.RawMessage `json:"findings"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal findings: %v", err)
	}
	if len(payload.Findings) != 1 {
		t.Fatalf("unexpected findings payload: %s", data)
	}
	finding = payload.Findings[0]
	for _, property := range []string{"patch", "paths"} {
		value, ok := finding[property]
		if !ok || string(value) != "null" {
			t.Fatalf("serialized finding %s = %s, want explicit null", property, value)
		}
	}
}
