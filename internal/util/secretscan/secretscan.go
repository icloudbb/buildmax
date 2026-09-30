// Package secretscan recognizes common secret shapes in free text.
//
// It is deliberately conservative -- keyword and shape based -- so it does not
// mangle ordinary tool output or refuse ordinary prose. Nothing here proves
// text is safe: a scanner that found nothing has found nothing, not established
// that a string holds no credential. Both callers treat it that way. The run
// trace redacts what it recognizes and still bounds and scopes what it writes;
// project memory refuses a write it recognizes and still tells the agent that
// not persisting credentials is its own contract.
//
// Grow the pattern set here rather than in either caller, so what the two
// recognize cannot drift apart.
package secretscan

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// pattern is one recognizable secret shape and how a redaction replaces it.
type pattern struct {
	name string
	re   *regexp.Regexp
	repl string
}

var patterns = []pattern{
	// "Bearer <token>"
	{"bearer token", regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._\-]+`), "Bearer [redacted]"},
	// OpenAI-style "sk-..." keys (and sk-proj- etc.)
	{"api key", regexp.MustCompile(`sk-[A-Za-z0-9._\-]{16,}`), "[redacted]"},
	// key=value / key: value where the key looks sensitive. The value run stops
	// at whitespace, quotes, commas, or closing brackets so JSON stays parseable.
	{
		"credential assignment",
		regexp.MustCompile(`(?i)\b(authorization|api[_\-]?key|access[_\-]?token|token|secret|password|passwd|pwd)\b(\s*["']?\s*[:=]\s*["']?)([^\s"',}\]]+)`),
		"$1$2[redacted]",
	},
	// PEM private key blocks, whatever the algorithm prefix.
	{"private key block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`), "[redacted]"},
}

// Redact returns s with recognized secret values replaced by a marker. Empty
// input is returned unchanged.
func Redact(s string) string {
	if s == "" {
		return s
	}
	for _, p := range patterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}

const (
	// minExactValue is the shortest exact value worth redacting. Below it, a
	// value is as likely to be an ordinary word as a credential, and replacing
	// every "abc" in output would mangle more than it protects. See
	// docs/design/space-secrets.md §12.
	minExactValue = 6
	// maxExactValue bounds a single exact value so one very large credential
	// (a certificate, a key file) cannot make every redaction pass unbounded.
	maxExactValue = 4096
	// exactMarker replaces a registered exact value wherever it is redacted.
	exactMarker = "[redacted]"
)

// Redactor redacts both recognized secret shapes and a fixed set of exact
// values. The exact set is a run's materialized Space Secret values, registered
// before the Agent starts so they do not drift into a durable trace, a log, a
// tool result, the live stream, or what the run reports and stores. It is defense in depth, not a boundary: a value can be encoded
// or transformed past it, which is why the primary control is withholding the
// value from the general environment. See docs/design/space-secrets.md §12.
type Redactor struct {
	exact []string
}

// NewRedactor builds a Redactor over the given exact values, dropping empty,
// very short, and oversized ones. A Redactor with no usable values redacts by
// shape only, exactly like the package Redact.
func NewRedactor(values []string) *Redactor {
	var exact []string
	for _, v := range values {
		if len(v) >= minExactValue && len(v) <= maxExactValue {
			exact = append(exact, v)
		}
	}
	return &Redactor{exact: exact}
}

// Redact replaces exact registered values first, then recognized shapes. A nil
// Redactor redacts by shape only, so a caller never needs to nil-check.
func (r *Redactor) Redact(s string) string {
	return Redact(r.RedactExact(s))
}

// RedactExact replaces only the registered exact values, not recognized shapes.
// It is for a sink where shape-based redaction would mangle output a consumer
// still needs -- a tool result the model must read to continue its work, where
// blanking every token-shaped substring would break the run. A nil Redactor
// returns s unchanged.
func (r *Redactor) RedactExact(s string) string {
	if r == nil || s == "" {
		return s
	}
	for _, v := range r.exact {
		s = strings.ReplaceAll(s, v, exactMarker)
	}
	return s
}

// RedactExactQuoted is RedactExact for a text log, which also holds a value in
// its Go-quoted form: slog's text handler quotes an attribute with a space, a
// quote, or a newline, so a multi-line value such as a key file appears only
// escaped. Both the raw value and that escaped body are replaced. A nil
// Redactor returns s unchanged.
func (r *Redactor) RedactExactQuoted(s string) string {
	s = r.RedactExact(s)
	if r == nil || s == "" {
		return s
	}
	for _, v := range r.exact {
		q := strconv.Quote(v)
		if body := q[1 : len(q)-1]; body != v {
			s = strings.ReplaceAll(s, body, exactMarker)
		}
	}
	return s
}

// RedactExactJSON applies RedactExact to the strings of one JSON document --
// its string values and object keys, after unescaping -- and re-encodes it only
// when something was replaced. Matching the decoded strings is the point: a
// value holding a quote, a backslash, or a character the encoder escapes is not
// present verbatim in the document's bytes, and replacing inside those bytes
// could cut a number or a structural token and leave the document unreadable.
//
// Numbers and literals are left alone. In the documents this serves they are
// structural -- sequence numbers, token counts -- and rewriting one into a
// string would break the reader, so a value the model emits as a bare JSON
// number is not caught here. Input that is not a single JSON document is
// redacted as plain text. A nil Redactor, or one with no exact values, returns
// doc unchanged.
func (r *Redactor) RedactExactJSON(doc []byte) []byte {
	if r == nil || len(r.exact) == 0 || len(doc) == 0 {
		return doc
	}
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return []byte(r.RedactExact(string(doc)))
	}
	if _, err := dec.Token(); err != io.EOF {
		return []byte(r.RedactExact(string(doc)))
	}
	v, changed := r.redactJSONValue(v)
	if !changed {
		return doc
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return []byte(r.RedactExact(string(doc)))
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

func (r *Redactor) redactJSONValue(v any) (any, bool) {
	switch t := v.(type) {
	case string:
		s := r.RedactExact(t)
		return s, s != t
	case []any:
		changed := false
		for i, e := range t {
			ne, c := r.redactJSONValue(e)
			t[i] = ne
			changed = changed || c
		}
		return t, changed
	case map[string]any:
		out := make(map[string]any, len(t))
		changed := false
		for k, e := range t {
			nk := r.RedactExact(k)
			ne, c := r.redactJSONValue(e)
			out[nk] = ne
			changed = changed || c || nk != k
		}
		return out, changed
	default:
		return v, false
	}
}

// Findings names the secret shapes recognized in s, in the order the patterns
// are declared and without repeats. It returns the names rather than the
// matches so a caller can say what it refused without quoting the credential
// back into a log, an error, or a model's context.
func Findings(s string) []string {
	if s == "" {
		return nil
	}
	var found []string
	for _, p := range patterns {
		if p.re.MatchString(s) {
			found = append(found, p.name)
		}
	}
	return found
}
