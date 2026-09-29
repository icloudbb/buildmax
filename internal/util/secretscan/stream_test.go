package secretscan

import (
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"
)

// streamThrough feeds pieces to a fresh StreamRedactor and returns what it
// emitted, joined, with every emitted piece checked to be whole UTF-8.
func streamThrough(t *testing.T, r *Redactor, pieces ...string) string {
	t.Helper()
	s := r.Stream()
	var out strings.Builder
	for _, p := range pieces {
		got := s.Write(p)
		if !utf8.ValidString(got) {
			t.Fatalf("emitted a piece that splits a character: %q", got)
		}
		out.WriteString(got)
	}
	out.WriteString(s.Flush())
	return out.String()
}

// The defect this exists for: a value split across two deltas reached the
// watcher whole because each delta was redacted on its own. Every split point,
// and every three-way split, of a value inside surrounding text must come out
// redacted, and the text around it must survive intact.
func TestStreamRedactorValueSplitAtEveryBoundary(t *testing.T) {
	const value = "ghs_Secret123456"
	r := NewRedactor([]string{value})
	text := "before " + value + " after"
	want := "before [redacted] after"
	for i := 0; i <= len(text); i++ {
		if got := streamThrough(t, r, text[:i], text[i:]); got != want {
			t.Fatalf("split at %d: got %q, want %q", i, got, want)
		}
		for j := i; j <= len(text); j++ {
			if got := streamThrough(t, r, text[:i], text[i:j], text[j:]); got != want {
				t.Fatalf("split at %d,%d: got %q, want %q", i, j, got, want)
			}
		}
	}
	// One byte at a time is the worst case a provider can produce.
	if got := streamThrough(t, r, strings.Split(text, "")...); got != want {
		t.Fatalf("byte by byte: got %q, want %q", got, want)
	}
}

func TestStreamRedactorMultipleAndOverlappingValues(t *testing.T) {
	// "abcdef12" is a prefix of "abcdef1234", and "cdef12zz" overlaps both.
	r := NewRedactor([]string{"abcdef12", "abcdef1234", "cdef12zz", "tok-XYZ-999"})
	cases := []struct{ text, want string }{
		{"x abcdef1234 y", "x [redacted] y"},
		{"x abcdef12 y", "x [redacted] y"},
		{"x cdef12zz y", "x [redacted] y"},
		{"tok-XYZ-999 and abcdef12", "[redacted] and [redacted]"},
		{"abcdef12abcdef12", "[redacted][redacted]"},
		// A near miss is released unchanged once it can no longer match.
		{"abcdef1 and tok-XYZ-99!", "abcdef1 and tok-XYZ-99!"},
	}
	for _, c := range cases {
		for i := 0; i <= len(c.text); i++ {
			got := streamThrough(t, r, c.text[:i], c.text[i:])
			if got != c.want {
				t.Fatalf("%q split at %d: got %q, want %q", c.text, i, got, c.want)
			}
		}
		// Whatever the split, no registered value may appear in what was emitted.
		got := streamThrough(t, r, strings.Split(c.text, "")...)
		for _, v := range r.exact {
			if strings.Contains(got, v) && !strings.Contains(c.want, v) {
				t.Fatalf("%q byte by byte leaked %q: %q", c.text, v, got)
			}
		}
	}
}

// A multi-byte value split inside a character is still caught, and text
// around it is never emitted with a character cut in half.
func TestStreamRedactorUTF8(t *testing.T) {
	const value = "密钥-ключ-🔑-42"
	r := NewRedactor([]string{value})
	text := "值是 " + value + " 结束。"
	want := "值是 [redacted] 结束。"
	for i := 0; i <= len(text); i++ {
		if got := streamThrough(t, r, text[:i], text[i:]); got != want {
			t.Fatalf("split at byte %d: got %q, want %q", i, got, want)
		}
	}
	if got := streamThrough(t, r, strings.Split(text, "")...); got != want {
		// strings.Split with "" splits by character; split by byte as well.
		t.Fatalf("by character: got %q, want %q", got, want)
	}
	pieces := make([]string, 0, len(text))
	for i := 0; i < len(text); i++ {
		pieces = append(pieces, text[i:i+1])
	}
	if got := streamThrough(t, r, pieces...); got != want {
		t.Fatalf("by byte: got %q, want %q", got, want)
	}
}

// The held tail is released by Flush, redacted or not, and a Flush with
// nothing held emits nothing.
func TestStreamRedactorFlushReleasesTheTail(t *testing.T) {
	r := NewRedactor([]string{"secret-value"})
	s := r.Stream()
	if got := s.Write("done: secret-"); got != "done: " {
		t.Fatalf("Write = %q, want the text before the possible value", got)
	}
	if got := s.Flush(); got != "secret-" {
		t.Fatalf("Flush = %q, want the held tail", got)
	}
	if got := s.Flush(); got != "" {
		t.Fatalf("second Flush = %q, want nothing", got)
	}
	// After a flush the redactor starts clean: nothing held joins the next text.
	if got := s.Write("value"); got != "value" {
		t.Fatalf("Write after Flush = %q", got)
	}
	// Text with no possible value is emitted as it arrives, not held.
	if got := s.Write(" and more"); got != " and more" {
		t.Fatalf("Write = %q, want it emitted at once", got)
	}
}

// With no Secret values there is nothing to redact and nothing to hold: the
// stream redactor is nil and passes every piece straight through.
func TestStreamRedactorNoValuesPassesThrough(t *testing.T) {
	for _, r := range []*Redactor{nil, NewRedactor(nil), NewRedactor([]string{"short"})} {
		s := r.Stream()
		if s != nil {
			t.Fatalf("Stream() = %v, want nil for a redactor with no exact values", s)
		}
		if got := s.Write("any sk-abcdefghijklmnop123 text"); got != "any sk-abcdefghijklmnop123 text" {
			t.Fatalf("nil Write = %q", got)
		}
		if got := s.Flush(); got != "" {
			t.Fatalf("nil Flush = %q", got)
		}
	}
}

// Where the pieces split never changes what comes out: every decision the
// redactor makes is one more text could not reverse, so the streamed result is
// the one-shot result. Checked over random text on a three-letter alphabet,
// where values overlap and prefix each other as densely as they can.
func TestStreamRedactorOutputIsIndependentOfSplits(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	values := []string{"abcabc", "abcabcab", "bcabca", "aabbcc", "cccccc"}
	r := NewRedactor(values)
	for range 2000 {
		b := make([]byte, rng.IntN(40))
		for i := range b {
			b[i] = "abc"[rng.IntN(3)]
		}
		text := string(b)
		whole := streamThrough(t, r, text)
		var pieces []string
		for rest := text; rest != ""; {
			k := 1 + rng.IntN(len(rest))
			pieces = append(pieces, rest[:k])
			rest = rest[k:]
		}
		if got := streamThrough(t, r, pieces...); got != whole {
			t.Fatalf("%q split as %q: got %q, one-shot %q", text, pieces, got, whole)
		}
		for _, v := range values {
			if strings.Contains(whole, v) {
				t.Fatalf("%q leaked %q: %q", text, v, whole)
			}
		}
	}
}

func TestRedactExactJSON(t *testing.T) {
	r := NewRedactor([]string{`pa"ss\word<1>`, "123456789"})
	// The value is escaped in the document's bytes; it is matched decoded, and
	// the result is still a valid document.
	doc := []byte(`{"text":"key is pa\"ss\\word<1> ok","n":123456789,"list":["pa\"ss\\word<1>"]}`)
	got := string(r.RedactExactJSON(doc))
	want := `{"list":["[redacted]"],"n":123456789,"text":"key is [redacted] ok"}`
	if got != want {
		t.Fatalf("RedactExactJSON = %s, want %s", got, want)
	}
	// A document with nothing to redact is returned byte for byte.
	clean := []byte(`{"b": 1, "a": "fine"}`)
	if got := r.RedactExactJSON(clean); string(got) != string(clean) {
		t.Fatalf("unchanged document was rewritten: %s", got)
	}
	// Not a single JSON document: redacted as text.
	if got := string(r.RedactExactJSON([]byte(`{"torn": "123456789`))); got != `{"torn": "[redacted]` {
		t.Fatalf("torn record = %s", got)
	}
	var nilR *Redactor
	if got := string(nilR.RedactExactJSON(doc)); got != string(doc) {
		t.Fatalf("nil redactor changed the document: %s", got)
	}
}
