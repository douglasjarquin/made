package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func decodeStrictParams(params json.RawMessage, out any) error {
	trimmed := bytes.TrimSpace(params)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] != '{' {
		return fmt.Errorf("params must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("params contain multiple JSON values")
		}
		return err
	}
	return nil
}
