package secretscan

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactor_ExactValues(t *testing.T) {
	r := NewRedactor([]string{"ghs_abcdef123456", "short", "", "AKIAIOSFODNN7EXAMPLE"})
	// A registered value is replaced wherever it appears.
	if got := r.Redact("token is ghs_abcdef123456 in the log"); got != "token is [redacted] in the log" {
		t.Fatalf("exact redaction: got %q", got)
	}
	// A value below the length floor is not registered, so it is not replaced
	// just for being in the list.
	if got := r.Redact("this is short here"); got != "this is short here" {
		t.Fatalf("short value should not be redacted: got %q", got)
	}
	// Shape-based redaction still runs on top of exact.
	if got := r.Redact("Authorization: Bearer sometoken12345"); got == "Authorization: Bearer sometoken12345" {
		t.Fatalf("shape redaction should still apply: got %q", got)
	}
}

// A text log quotes an attribute that holds a newline or a quote, so a
// multi-line value is present only escaped; the quoted form must still go.
func TestRedactor_ExactQuotedCoversTheTextLogForm(t *testing.T) {
	const value = "line one of the harbor canary\nline \"two\""
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("tool output", "text", value, "plain", "harbor-canary-value")
	r := NewRedactor([]string{value, "harbor-canary-value"})
	got := r.RedactExactQuoted(buf.String())
	if strings.Contains(got, "harbor canary") || strings.Contains(got, "harbor-canary-value") {
		t.Fatalf("log line still carries a value: %q", got)
	}
	if !strings.Contains(got, `text="[redacted]"`) || !strings.Contains(got, "plain=[redacted]") {
		t.Fatalf("log line = %q, want both values replaced in place", got)
	}
	var nilRedactor *Redactor
	if got := nilRedactor.RedactExactQuoted(buf.String()); got != buf.String() {
		t.Fatalf("nil redactor changed the log: %q", got)
	}
}

func TestRedactor_NilRedactsByShapeOnly(t *testing.T) {
	var r *Redactor
	if got := r.Redact("sk-abcdefghijklmnop123"); got == "sk-abcdefghijklmnop123" {
		t.Fatalf("nil redactor should still redact shapes: got %q", got)
	}
}
