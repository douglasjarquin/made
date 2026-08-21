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
	// Optional fields added for managed-validation structured output.
	// Existing agents that do not emit these fields continue to work.
	Code   string `json:"code,omitempty"`
	Class  string `json:"class,omitempty"`
	Symbol string `json:"symbol,omitempty"`
}

func (f Finding) MarshalJSON() ([]byte, error) {
	var patch *string
	if f.Patch != "" {
		patch = &f.Patch
	}
	var paths []string
	if f.Paths != nil {
		paths = append([]string(nil), f.Paths...)
	}
	return json.Marshal(struct {
		Kind        FindingKind `json:"kind"`
		Description string      `json:"description"`
		Patch       *string     `json:"patch"`
		Paths       []string    `json:"paths"`
		Code        string      `json:"code,omitempty"`
		Class       string      `json:"class,omitempty"`
		Symbol      string      `json:"symbol,omitempty"`
	}{
		Kind:        f.Kind,
		Description: f.Description,
		Patch:       patch,
		Paths:       paths,
		Code:        f.Code,
		Class:       f.Class,
		Symbol:      f.Symbol,
	})
}

func (f *Finding) UnmarshalJSON(data []byte) error {
	var wire struct {
		Kind        *FindingKind    `json:"kind"`
		Description *string         `json:"description"`
		Patch       json.RawMessage `json:"patch"`
		Paths       json.RawMessage `json:"paths"`
		Code        string          `json:"code"`
		Class       string          `json:"class"`
		Symbol      string          `json:"symbol"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	if wire.Kind == nil || wire.Description == nil || len(wire.Patch) == 0 || len(wire.Paths) == 0 {
		return fmt.Errorf("finding requires kind, description, patch, and paths")
	}
	f.Kind = *wire.Kind
	f.Description = *wire.Description
	f.Patch = ""
	f.Paths = nil
	f.Code = wire.Code
	f.Class = wire.Class
	f.Symbol = wire.Symbol
	if !bytes.Equal(bytes.TrimSpace(wire.Patch), []byte("null")) {
		if err := json.Unmarshal(wire.Patch, &f.Patch); err != nil {
			return fmt.Errorf("finding patch must be a string or null: %w", err)
		}
	}
	if !bytes.Equal(bytes.TrimSpace(wire.Paths), []byte("null")) {
		var paths []string
		if err := json.Unmarshal(wire.Paths, &paths); err != nil {
			return fmt.Errorf("finding paths must be an array or null: %w", err)
		}
		f.Paths = append([]string(nil), paths...)
	}
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
