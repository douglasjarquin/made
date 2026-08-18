package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type FindingKind string

const (
	FindingAutoFixable FindingKind = "auto-fixable"
	FindingAskUser     FindingKind = "ask-user"
	FindingBlocking    FindingKind = "blocking"
)

type Finding struct {
	Kind        FindingKind `json:"kind"`
	Description string      `json:"description"`
	Patch       string      `json:"patch,omitempty"`
	Paths       []string    `json:"paths,omitempty"`
}

func (f *Finding) UnmarshalJSON(data []byte) error {
	var wire struct {
		Kind        *FindingKind `json:"kind"`
		Description *string      `json:"description"`
		Patch       *string      `json:"patch"`
		Paths       []string     `json:"paths"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	if wire.Kind == nil || wire.Description == nil {
		return fmt.Errorf("finding requires kind and description")
	}
	f.Kind = *wire.Kind
	f.Description = *wire.Description
	f.Patch = ""
	if wire.Patch != nil {
		f.Patch = *wire.Patch
	}
	f.Paths = append([]string(nil), wire.Paths...)
	return nil
}

type Findings struct {
	Findings []Finding `json:"findings"`
}

func (f *Findings) UnmarshalJSON(data []byte) error {
	var wire struct {
		Findings *[]Finding `json:"findings"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	if wire.Findings == nil {
		return fmt.Errorf("structured output requires findings")
	}
	f.Findings = append([]Finding(nil), (*wire.Findings)...)
	return nil
}

func (f Findings) MarshalJSON() ([]byte, error) {
	findings := f.Findings
	if findings == nil {
		findings = []Finding{}
	}
	type payload struct {
		Findings []Finding `json:"findings"`
	}
	return json.Marshal(payload{Findings: findings})
}
