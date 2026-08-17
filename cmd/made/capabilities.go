package main

import (
	"encoding/json"
	"fmt"
	"os"
)

const capabilitiesSchemaVersion = 1

type capabilitiesReport struct {
	SchemaVersion   int      `json:"schema_version"`
	ProtocolVersion int      `json:"protocol_version"`
	Commands        []string `json:"commands"`
}

func runCapabilitiesCommand(args []string, stdout, stderr *os.File) int {
	if len(args) != 1 || args[0] != "--json" {
		_, _ = fmt.Fprintln(stderr, "usage: made capabilities --json")
		return 2
	}
	report := capabilitiesReport{
		SchemaVersion:   capabilitiesSchemaVersion,
		ProtocolVersion: 1,
		Commands: []string{
			"run.submit",
			"run.status",
			"run.list",
			"run.cancel",
			"review.decide",
			"doctor",
		},
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		_, _ = fmt.Fprintln(stderr, "made capabilities:", err)
		return 1
	}
	return 0
}
