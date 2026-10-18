package toolschema

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

type sampleInput struct {
	TaskID string `json:"task_id" jsonschema:"Task ID" agentschema:"minLength=1,maxLength=64"`
	Note   string `json:"note,omitempty" jsonschema:"Optional note"`
}

type numericInput struct {
	Limit int    `json:"limit,omitempty" jsonschema:"Page size" agentschema:"minimum=1,maximum=100,default=30"`
	Owner string `json:"owner" jsonschema:"GitHub owner" agentschema:"minLength=1,pattern=^[a-zA-Z0-9_-]+$"`
}

type enumInput struct {
	Mode string `json:"mode" jsonschema:"Execution mode" agentschema:"enum=fast,accurate"`
}

type enumDefaultInput struct {
	Type string `json:"type,omitempty" jsonschema:"Property type" agentschema:"enum=string,number,default=string"`
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
	if c2.Pattern != "^[a-z]+$" {
		t.Fatalf("pattern = %q", c2.Pattern)
	}

	c3, err := ParseAgentschema("enum=opt_in,opt_out")
	if err != nil {
		t.Fatal(err)
	}
	if len(c3.Enum) != 2 || c3.Enum[0] != "opt_in" || c3.Enum[1] != "opt_out" {
		t.Fatalf("enum = %v", c3.Enum)
	}

	c4, err := ParseAgentschema("default=contacts.csv")
	if err != nil {
		t.Fatal(err)
	}
	if !c4.HasDefault {
		t.Fatal("expected HasDefault")
	}
	var s string
	if err := json.Unmarshal(c4.Default, &s); err != nil || s != "contacts.csv" {
		t.Fatalf("default = %q err=%v", c4.Default, err)
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
}

func TestFor_appliesNumericPatternDefaultAndEnum(t *testing.T) {
	s, err := For[numericInput](nil)
	if err != nil {
		t.Fatal(err)
	}
	limit := s.Properties["limit"]
	if limit == nil {
		t.Fatal("missing limit property")
	}
	if limit.Minimum == nil || *limit.Minimum != 1 || limit.Maximum == nil || *limit.Maximum != 100 {
		t.Fatalf("limit bounds = %+v", limit)
	}
	if len(limit.Default) == 0 {
		t.Fatal("expected default on limit")
	}
	var def int
	if err := json.Unmarshal(limit.Default, &def); err != nil || def != 30 {
		t.Fatalf("limit default = %v err=%v", limit.Default, err)
	}

	enumSchema, err := For[enumInput](nil)
	if err != nil {
		t.Fatal(err)
	}
	mode := enumSchema.Properties["mode"]
	if mode == nil || len(mode.Enum) != 2 {
		t.Fatalf("mode enum = %+v", mode)
	}

	mixed, err := For[enumDefaultInput](nil)
	if err != nil {
		t.Fatal(err)
	}
	typ := mixed.Properties["type"]
	if typ == nil || len(typ.Enum) != 2 {
		t.Fatalf("type enum = %+v", typ)
	}
	if len(typ.Default) == 0 {
		t.Fatal("expected default on type")
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

func TestMergeAgentschemaSegments_enumOnly(t *testing.T) {
	got := mergeAgentschemaSegments("enum=string,number")
	if len(got) != 1 || got[0] != "enum=string,number" {
		t.Fatalf("got %v", got)
	}
}
