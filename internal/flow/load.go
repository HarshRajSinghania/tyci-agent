package flow

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Parse parses and JSON-validates a workflow.
// Unknown fields are rejected so typos never pass silently.
func Parse(data []byte) (*Workflow, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var wf Workflow
	if err := dec.Decode(&wf); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	return &wf, nil
}
