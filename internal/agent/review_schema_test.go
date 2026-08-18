package agent

import (
	"encoding/json"
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
		found := false
		for _, required := range schema.Properties.Findings.Items.Required {
			if required == property {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("review schema does not require finding property %q", property)
		}
	}
}
