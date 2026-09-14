// Package jsonschema implements the single JSON Schema subset that BuildMax's
// provider-neutral structured output (docs/design/structured-output.md §6) and
// Workflow input schemas (docs/design/workflow-runtime.md §6.1) both reference.
// One definition, checked once, means the same thing at every boundary.
//
// The subset is deliberately narrow: objects, the scalar types
// (string/number/integer/boolean), enums, arrays, required, and
// additionalProperties:false. Every keyword must be both expressible in each
// provider's native mechanism and checkable here, so a keyword only one
// provider supports stays out until a consumer needs it. Growing the subset is
// adding a keyword when a consumer requires it, never before.
package jsonschema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
)

// Supported type keywords.
const (
	TypeObject  = "object"
	TypeArray   = "array"
	TypeString  = "string"
	TypeNumber  = "number"
	TypeInteger = "integer"
	TypeBoolean = "boolean"
)

// Schema is a compiled schema in the supported subset. Compile it once and
// validate many values against it.
type Schema struct {
	typ        string
	enum       []any // decoded allowed values, nil when the schema has no enum
	properties map[string]*Schema
	required   []string
	items      *Schema
}

// Compile parses raw and verifies it is within the supported subset, returning
// a reusable Schema or an error naming the first unsupported construct. Workflow
// publication uses it to reject an out-of-subset schema; the runtime uses the
// result to validate values.
func Compile(raw json.RawMessage) (*Schema, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("schema is empty")
	}
	return compileNode(raw, "")
}

// allowedKeywords is the whole subset. A key outside it is drift, not history:
// the runtime cannot check it and some provider cannot express it.
var allowedKeywords = map[string]bool{
	"type": true, "description": true, "enum": true,
	"properties": true, "required": true, "additionalProperties": true,
	"items": true,
}

func compileNode(raw json.RawMessage, path string) (*Schema, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("%s: schema must be a JSON object: %w", at(path), err)
	}
	for key := range fields {
		if !allowedKeywords[key] {
			return nil, fmt.Errorf("%s: unsupported keyword %q", at(path), key)
		}
	}

	typeRaw, ok := fields["type"]
	if !ok {
		return nil, fmt.Errorf("%s: schema must declare a type", at(path))
	}
	var typ string
	if err := json.Unmarshal(typeRaw, &typ); err != nil {
		return nil, fmt.Errorf("%s: type must be a string: %w", at(path), err)
	}

	s := &Schema{typ: typ}
	switch typ {
	case TypeObject:
		if err := s.compileObject(fields, path); err != nil {
			return nil, err
		}
	case TypeArray:
		if err := s.compileArray(fields, path); err != nil {
			return nil, err
		}
	case TypeString, TypeNumber, TypeInteger, TypeBoolean:
		if err := s.compileScalar(fields, path); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%s: unsupported type %q", at(path), typ)
	}
	return s, nil
}

func (s *Schema) compileObject(fields map[string]json.RawMessage, path string) error {
	if err := forbid(fields, path, TypeObject, "enum", "items"); err != nil {
		return err
	}
	apRaw, ok := fields["additionalProperties"]
	if !ok {
		return fmt.Errorf("%s: object schema must set additionalProperties: false", at(path))
	}
	var ap bool
	if err := json.Unmarshal(apRaw, &ap); err != nil || ap {
		return fmt.Errorf("%s: additionalProperties must be false", at(path))
	}

	propsRaw, ok := fields["properties"]
	if !ok {
		return fmt.Errorf("%s: object schema must declare properties", at(path))
	}
	var props map[string]json.RawMessage
	if err := json.Unmarshal(propsRaw, &props); err != nil {
		return fmt.Errorf("%s: properties must be an object: %w", at(path), err)
	}
	s.properties = make(map[string]*Schema, len(props))
	for name, propRaw := range props {
		child, err := compileNode(propRaw, join(path, name))
		if err != nil {
			return err
		}
		s.properties[name] = child
	}

	if reqRaw, ok := fields["required"]; ok {
		if err := json.Unmarshal(reqRaw, &s.required); err != nil {
			return fmt.Errorf("%s: required must be an array of strings: %w", at(path), err)
		}
		for _, name := range s.required {
			if _, declared := s.properties[name]; !declared {
				return fmt.Errorf("%s: required names %q, which is not a declared property", at(path), name)
			}
		}
	}
	return nil
}

func (s *Schema) compileArray(fields map[string]json.RawMessage, path string) error {
	if err := forbid(fields, path, TypeArray, "properties", "required", "additionalProperties", "enum"); err != nil {
		return err
	}
	itemsRaw, ok := fields["items"]
	if !ok {
		return fmt.Errorf("%s: array schema must declare items", at(path))
	}
	child, err := compileNode(itemsRaw, join(path, "[]"))
	if err != nil {
		return err
	}
	s.items = child
	return nil
}

func (s *Schema) compileScalar(fields map[string]json.RawMessage, path string) error {
	if err := forbid(fields, path, s.typ, "properties", "required", "additionalProperties", "items"); err != nil {
		return err
	}
	enumRaw, ok := fields["enum"]
	if !ok {
		return nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(enumRaw, &entries); err != nil {
		return fmt.Errorf("%s: enum must be an array: %w", at(path), err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("%s: enum must list at least one value", at(path))
	}
	for _, entry := range entries {
		v, err := decode(entry)
		if err != nil {
			return fmt.Errorf("%s: enum value: %w", at(path), err)
		}
		if err := s.checkScalarType(v); err != nil {
			return fmt.Errorf("%s: enum value does not match type %q: %w", at(path), s.typ, err)
		}
		s.enum = append(s.enum, v)
	}
	return nil
}

// forbid rejects keywords that do not apply to the type being compiled, so a
// mis-shaped schema fails at compile rather than silently ignoring a field.
func forbid(fields map[string]json.RawMessage, path, typ string, keys ...string) error {
	for _, key := range keys {
		if _, present := fields[key]; present {
			return fmt.Errorf("%s: keyword %q is not valid on a %q schema", at(path), key, typ)
		}
	}
	return nil
}

// Validate checks value against the schema, returning an error describing the
// first mismatch. A nil error means value conforms.
func (s *Schema) Validate(value json.RawMessage) error {
	v, err := decode(value)
	if err != nil {
		return fmt.Errorf("value is not valid JSON: %w", err)
	}
	return s.validateValue(v, "")
}

func (s *Schema) validateValue(v any, path string) error {
	switch s.typ {
	case TypeObject:
		return s.validateObject(v, path)
	case TypeArray:
		return s.validateArray(v, path)
	default:
		if err := s.checkScalarType(v); err != nil {
			return fmt.Errorf("%s: %w", at(path), err)
		}
		return s.checkEnum(v, path)
	}
}

func (s *Schema) validateObject(v any, path string) error {
	obj, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: expected object, got %s", at(path), kind(v))
	}
	for _, name := range s.required {
		if _, present := obj[name]; !present {
			return fmt.Errorf("%s: missing required property %q", at(path), name)
		}
	}
	// Keys are sorted so a schema with several unexpected keys fails
	// deterministically on the same one every run.
	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		child, allowed := s.properties[name]
		if !allowed {
			return fmt.Errorf("%s: unexpected property %q (additionalProperties is false)", at(path), name)
		}
		if err := child.validateValue(obj[name], join(path, name)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Schema) validateArray(v any, path string) error {
	arr, ok := v.([]any)
	if !ok {
		return fmt.Errorf("%s: expected array, got %s", at(path), kind(v))
	}
	for i, elem := range arr {
		if err := s.items.validateValue(elem, fmt.Sprintf("%s[%d]", pathOrRoot(path), i)); err != nil {
			return err
		}
	}
	return nil
}

// checkScalarType reports whether v matches this scalar schema's type. Numbers
// arrive as json.Number because values are decoded with UseNumber, so integer
// and number stay distinguishable and large integers keep their precision.
func (s *Schema) checkScalarType(v any) error {
	switch s.typ {
	case TypeString:
		if _, ok := v.(string); !ok {
			return fmt.Errorf("expected string, got %s", kind(v))
		}
	case TypeBoolean:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("expected boolean, got %s", kind(v))
		}
	case TypeNumber:
		if _, ok := v.(json.Number); !ok {
			return fmt.Errorf("expected number, got %s", kind(v))
		}
	case TypeInteger:
		n, ok := v.(json.Number)
		if !ok {
			return fmt.Errorf("expected integer, got %s", kind(v))
		}
		f, err := n.Float64()
		if err != nil || f != math.Trunc(f) {
			return fmt.Errorf("expected integer, got non-integer number %s", n.String())
		}
	}
	return nil
}

func (s *Schema) checkEnum(v any, path string) error {
	if s.enum == nil {
		return nil
	}
	for _, allowed := range s.enum {
		if reflect.DeepEqual(allowed, v) {
			return nil
		}
	}
	return fmt.Errorf("%s: value is not one of the allowed enum values", at(path))
}

// decode parses raw JSON with numbers kept as json.Number, so the validator can
// tell an integer from a number and enum comparison is consistent.
func decode(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func kind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case json.Number:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}

// at, join, and pathOrRoot render the location of an error. The root value is
// "(root)" so a top-level mismatch still names something.
func at(path string) string {
	if path == "" {
		return "(root)"
	}
	return path
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func pathOrRoot(path string) string {
	if path == "" {
		return ""
	}
	return path
}
