package toolschema

import (
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

type sampleInput struct {
	TaskID string `json:"task_id" jsonschema:"Task ID" agentschema:"minLength=1,maxLength=64"`
	Note   string `json:"note,omitempty" jsonschema:"Optional note"`
}

type numericInput struct {
	Limit int    `json:"limit,omitempty" jsonschema:"Page size" agentschema:"minimum=1,maximum=100"`
	Owner string `json:"owner" jsonschema:"GitHub owner" agentschema:"minLength=1,pattern=^[a-zA-Z0-9_-]+$"`
}

func TestParseAgentschema(t *testing.T) {
	c, err := ParseAgentschema("minLength=1,maxLength=100")
	if err != nil {
		t.Fatal(err)
	}
	if c.MinLength == nil || *c.MinLength != 1 {
		t.Fatalf("minLength = %v", c.MinLength)
	}
	if c.MaxLength == nil || *c.MaxLength != 100 {
		t.Fatalf("maxLength = %v", c.MaxLength)
	}
	if _, err := ParseAgentschema("bad"); err == nil {
		t.Fatal("expected error for bad segment")
	}

	c2, err := ParseAgentschema("minimum=1,maximum=100,pattern=^[a-z]+$")
	if err != nil {
		t.Fatal(err)
	}
	if c2.Minimum == nil || *c2.Minimum != 1 {
		t.Fatalf("minimum = %v", c2.Minimum)
	}
	if c2.Maximum == nil || *c2.Maximum != 100 {
		t.Fatalf("maximum = %v", c2.Maximum)
	}
	if c2.Pattern != "^[a-z]+$" {
		t.Fatalf("pattern = %q", c2.Pattern)
	}
	if _, err := ParseAgentschema("pattern=["); err == nil {
		t.Fatal("expected error for invalid pattern")
	}
}

func TestFor_appliesAgentschema(t *testing.T) {
	s, err := For[sampleInput](nil)
	if err != nil {
		t.Fatal(err)
	}
	p := s.Properties["task_id"]
	if p == nil {
		t.Fatal("missing task_id property")
	}
	if p.MinLength == nil || *p.MinLength != 1 {
		t.Fatalf("task_id minLength = %v", p.MinLength)
	}
	if p.MaxLength == nil || *p.MaxLength != 64 {
		t.Fatalf("task_id maxLength = %v", p.MaxLength)
	}
	note := s.Properties["note"]
	if note == nil {
		t.Fatal("missing note property")
	}
	if note.MinLength != nil {
		t.Fatalf("note should not have minLength")
	}
}

func TestFor_matchesJsonschemaForDescriptions(t *testing.T) {
	base, err := jsonschema.For[sampleInput](nil)
	if err != nil {
		t.Fatal(err)
	}
	withConstraints, err := For[sampleInput](nil)
	if err != nil {
		t.Fatal(err)
	}
	if base.Properties["task_id"].Description != withConstraints.Properties["task_id"].Description {
		t.Fatal("description should match jsonschema.For")
	}
}

func TestFor_appliesNumericAndPatternAgentschema(t *testing.T) {
	s, err := For[numericInput](nil)
	if err != nil {
		t.Fatal(err)
	}
	limit := s.Properties["limit"]
	if limit == nil {
		t.Fatal("missing limit property")
	}
	if limit.Minimum == nil || *limit.Minimum != 1 {
		t.Fatalf("limit minimum = %v", limit.Minimum)
	}
	if limit.Maximum == nil || *limit.Maximum != 100 {
		t.Fatalf("limit maximum = %v", limit.Maximum)
	}
	owner := s.Properties["owner"]
	if owner == nil {
		t.Fatal("missing owner property")
	}
	if owner.Pattern != "^[a-zA-Z0-9_-]+$" {
		t.Fatalf("owner pattern = %q", owner.Pattern)
	}
}
