package connectortest

import (
	"testing"

	"github.com/agentruntime-io/agentruntime-mcp-go/toolschema"
	"github.com/google/jsonschema-go/jsonschema"
)

type sampleIDInput struct {
	TaskID string `json:"task_id" jsonschema:"Task ID" agentschema:"minLength=1"`
	Name   string `json:"name,omitempty" jsonschema:"Optional name"`
}

func TestAssertRequiredStringIDsHaveMinLength(t *testing.T) {
	schema, err := toolschema.For[sampleIDInput](nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := AssertRequiredStringIDsHaveMinLength(schema); err != nil {
		t.Fatal(err)
	}
}

func TestAssertRequiredStringIDsHaveMinLength_missing(t *testing.T) {
	schema := &jsonschema.Schema{
		Type:     "object",
		Required: []string{"task_id"},
		Properties: map[string]*jsonschema.Schema{
			"task_id": {Type: "string"},
		},
	}
	if err := AssertRequiredStringIDsHaveMinLength(schema); err == nil {
		t.Fatal("expected error for missing minLength on task_id")
	}
}
