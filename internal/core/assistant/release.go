package assistant

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// maxReleasedRunes bounds the released text, so one large field cannot flood
// a chat or the front-door model's context.
const maxReleasedRunes = 1500

// Field is one released top-level property of a result.
type Field struct {
	Name  string
	Value json.RawMessage
}

// Entry returns the roster entry for kind and id, or nil when the roster no
// longer has it. Nothing is released for work whose entry is gone: the owner
// confirmed the current roster, not the one the work started under.
func (d Definition) Entry(kind, id string) *RosterEntry {
	for i := range d.Roster {
		if d.Roster[i].Kind == kind && d.Roster[i].ID == id {
			return &d.Roster[i]
		}
	}
	return nil
}

// Release keeps only the releasable top-level properties of a structured
// result, in the order the contract lists them. A result that is not a JSON
// object releases nothing. This is the one place a requester's view of a
// result is decided (docs/design/space-assistants.md §8).
func Release(result *string, entry *RosterEntry) []Field {
	if result == nil || entry == nil || len(entry.Releasable) == 0 {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(*result), &obj); err != nil {
		return nil
	}
	var out []Field
	for _, name := range entry.Releasable {
		if v, ok := obj[name]; ok {
			out = append(out, Field{Name: name, Value: v})
		}
	}
	return out
}

// FormatReleased renders released fields as "name: value" lines: a string as
// its text, anything else as compact JSON. It is bounded.
func FormatReleased(fields []Field) string {
	var b strings.Builder
	for i, f := range fields {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(f.Name)
		b.WriteString(": ")
		var s string
		if err := json.Unmarshal(f.Value, &s); err == nil {
			b.WriteString(s)
			continue
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, f.Value); err == nil {
			b.Write(compact.Bytes())
		}
	}
	out := b.String()
	if utf8.RuneCountInString(out) > maxReleasedRunes {
		out = string([]rune(out)[:maxReleasedRunes]) + "…"
	}
	return out
}
