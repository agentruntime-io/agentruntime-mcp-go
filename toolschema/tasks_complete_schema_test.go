package toolschema_test

import (
	"encoding/json"
	"testing"

	"github.com/agentruntime-io/agentruntime-mcp-go/toolschema"
)

type tasksCompleteInput struct {
	WorkflowID   string            `json:"workflow_id" jsonschema:"required"`
	RunID        string            `json:"run_id" jsonschema:"required"`
	TaskID       string            `json:"task_id" jsonschema:"required"`
	Approved     bool              `json:"approved" jsonschema:"required"`
	Result       map[string]any    `json:"result,omitempty"`
	ContextEdits map[string]string `json:"context_edits,omitempty"`
}

func TestTasksCompleteSchema_hasContextEdits_stringMap(t *testing.T) {
	schema, err := toolschema.For[tasksCompleteInput](nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(schema, "", "  ")
	if schema.Properties["context_edits"] == nil {
		t.Fatalf("context_edits missing:\n%s", string(b))
	}
}
