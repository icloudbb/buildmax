package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalidPointer means a string is not a syntactically valid RFC 6901 JSON
// Pointer. ErrPointerMiss means a valid pointer does not resolve against a
// particular document -- a referenced key or array index is absent. The two are
// distinct so publication can reject bad syntax while a run reports a binding
// that resolved against real state to nothing.
var (
	ErrInvalidPointer = errors.New("invalid RFC 6901 JSON pointer")
	ErrPointerMiss    = errors.New("JSON pointer does not resolve")
)

// ValidatePointer reports whether pointer is a syntactically valid RFC 6901 JSON
// Pointer. The empty string (the whole document) is valid; any non-empty pointer
// must begin with "/" and use only the "~0"/"~1" escapes.
func ValidatePointer(pointer string) error {
	if pointer == "" {
		return nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return fmt.Errorf("%w: %q must be empty or begin with '/'", ErrInvalidPointer, pointer)
	}
	for _, tok := range strings.Split(pointer[1:], "/") {
		if err := validateToken(tok); err != nil {
			return err
		}
	}
	return nil
}

// validateToken checks that a reference token uses "~" only as the "~0"/"~1"
// escape, the one place RFC 6901 constrains a token's characters.
func validateToken(tok string) error {
	for i := 0; i < len(tok); i++ {
		if tok[i] != '~' {
			continue
		}
		if i+1 >= len(tok) || (tok[i+1] != '0' && tok[i+1] != '1') {
			return fmt.Errorf("%w: %q has a '~' not followed by 0 or 1", ErrInvalidPointer, tok)
		}
		i++
	}
	return nil
}

// ResolvePointer returns the value pointer selects from doc, as JSON text. The
// empty pointer returns doc unchanged. A syntactically invalid pointer is
// ErrInvalidPointer; a valid pointer that names an absent key or index is
// ErrPointerMiss. doc must be valid JSON.
func ResolvePointer(doc json.RawMessage, pointer string) (json.RawMessage, error) {
	if err := ValidatePointer(pointer); err != nil {
		return nil, err
	}
	if pointer == "" {
		return doc, nil
	}
	var current any
	if err := json.Unmarshal(doc, &current); err != nil {
		return nil, fmt.Errorf("resolve pointer: %w", err)
	}
	for _, tok := range strings.Split(pointer[1:], "/") {
		ref := unescapeToken(tok)
		next, err := step(current, ref)
		if err != nil {
			return nil, err
		}
		current = next
	}
	out, err := json.Marshal(current)
	if err != nil {
		return nil, fmt.Errorf("resolve pointer: %w", err)
	}
	return out, nil
}

// step descends one reference token into an object or array. An object member is
// looked up by key; an array element by a base-10 index within bounds. Anything
// else -- a token into a scalar, a non-numeric array index, an out-of-range
// index -- is a miss.
func step(current any, ref string) (any, error) {
	switch node := current.(type) {
	case map[string]any:
		v, ok := node[ref]
		if !ok {
			return nil, fmt.Errorf("%w: no member %q", ErrPointerMiss, ref)
		}
		return v, nil
	case []any:
		idx, err := strconv.Atoi(ref)
		if err != nil || idx < 0 || idx >= len(node) {
			return nil, fmt.Errorf("%w: index %q out of range", ErrPointerMiss, ref)
		}
		return node[idx], nil
	default:
		return nil, fmt.Errorf("%w: cannot descend %q into a scalar", ErrPointerMiss, ref)
	}
}

// unescapeToken applies the RFC 6901 unescaping: "~1" is "/" and "~0" is "~",
// decoded in that order so an escaped "~1" round-trips.
func unescapeToken(tok string) string {
	tok = strings.ReplaceAll(tok, "~1", "/")
	return strings.ReplaceAll(tok, "~0", "~")
}
