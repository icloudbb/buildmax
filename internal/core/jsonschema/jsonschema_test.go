package jsonschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompileRejectsOutOfSubset(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		want   string // substring the error must name
	}{
		{"unknown keyword", `{"type":"string","pattern":"x"}`, "unsupported keyword"},
		{"missing type", `{"description":"x"}`, "must declare a type"},
		{"unsupported type", `{"type":"null"}`, "unsupported type"},
		{"object without additionalProperties", `{"type":"object","properties":{}}`, "additionalProperties: false"},
		{"object additionalProperties true", `{"type":"object","properties":{},"additionalProperties":true}`, "must be false"},
		{"object without properties", `{"type":"object","additionalProperties":false}`, "must declare properties"},
		{"required names unknown property", `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string"}},"required":["b"]}`, "not a declared property"},
		{"array without items", `{"type":"array"}`, "must declare items"},
		{"enum on object", `{"type":"object","additionalProperties":false,"properties":{},"enum":[1]}`, "not valid on"},
		{"empty enum", `{"type":"string","enum":[]}`, "at least one value"},
		{"enum type mismatch", `{"type":"string","enum":[1]}`, "does not match type"},
		{"empty schema", ``, "empty"},
		{"nested unsupported keyword", `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string","format":"email"}}}`, "unsupported keyword"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(json.RawMessage(tc.schema))
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

func TestCompileAcceptsSubset(t *testing.T) {
	schemas := []string{
		`{"type":"string"}`,
		`{"type":"integer","enum":[1,2,3]}`,
		`{"type":"string","enum":["a","b"],"description":"a choice"}`,
		`{"type":"array","items":{"type":"number"}}`,
		`{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string"},"age":{"type":"integer"}},"required":["name"]}`,
		`{"type":"object","additionalProperties":false,"properties":{"tags":{"type":"array","items":{"type":"string"}}}}`,
	}
	for _, s := range schemas {
		if _, err := Compile(json.RawMessage(s)); err != nil {
			t.Errorf("Compile(%s) = %v, want nil", s, err)
		}
	}
}

func TestValidate(t *testing.T) {
	const objectSchema = `{
		"type":"object",
		"additionalProperties":false,
		"properties":{
			"name":{"type":"string"},
			"age":{"type":"integer"},
			"role":{"type":"string","enum":["admin","user"]},
			"tags":{"type":"array","items":{"type":"string"}}
		},
		"required":["name","role"]
	}`
	schema, err := Compile(json.RawMessage(objectSchema))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	valid := []string{
		`{"name":"a","role":"admin"}`,
		`{"name":"a","role":"user","age":30,"tags":["x","y"]}`,
	}
	for _, v := range valid {
		if err := schema.Validate(json.RawMessage(v)); err != nil {
			t.Errorf("Validate(%s) = %v, want nil", v, err)
		}
	}

	invalid := []struct {
		value string
		want  string
	}{
		{`{"role":"admin"}`, "missing required property \"name\""},
		{`{"name":"a"}`, "missing required property \"role\""},
		{`{"name":"a","role":"admin","extra":1}`, "unexpected property \"extra\""},
		{`{"name":1,"role":"admin"}`, "expected string"},
		{`{"name":"a","role":"admin","age":1.5}`, "non-integer"},
		{`{"name":"a","role":"root"}`, "not one of the allowed enum"},
		{`{"name":"a","role":"admin","tags":[1]}`, "expected string"},
		{`["not","an","object"]`, "expected object"},
		{`{`, "not valid JSON"},
	}
	for _, tc := range invalid {
		err := schema.Validate(json.RawMessage(tc.value))
		if err == nil {
			t.Errorf("Validate(%s) = nil, want error", tc.value)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Validate(%s) error %q does not mention %q", tc.value, err.Error(), tc.want)
		}
	}
}

// TestValidateIntegerBoundary pins that a whole-valued number satisfies integer
// while a fractional one does not, since both arrive as json.Number.
func TestValidateIntegerBoundary(t *testing.T) {
	schema, err := Compile(json.RawMessage(`{"type":"integer"}`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if err := schema.Validate(json.RawMessage(`42`)); err != nil {
		t.Errorf("Validate(42) = %v, want nil", err)
	}
	if err := schema.Validate(json.RawMessage(`42.0`)); err != nil {
		t.Errorf("Validate(42.0) = %v, want nil (whole-valued)", err)
	}
	if err := schema.Validate(json.RawMessage(`42.5`)); err == nil {
		t.Errorf("Validate(42.5) = nil, want error")
	}
}
