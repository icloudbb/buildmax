package secretscan

import (
	"strings"
	"unicode/utf8"
)

// StreamRedactor applies a Redactor's exact values to text that arrives in
// pieces, such as a model reply streamed token by token.
//
// Redacting each piece on its own misses a value the provider split across two
// of them, and a model writes a credential as several tokens, so the joined
// stream a watcher reads would carry it whole. This holds back only a tail that
// could still become a value -- a proper prefix of one, or the leading bytes of
// an incomplete UTF-8 character -- emits everything before it redacted, and
// releases the tail on Flush. What it emits, joined, never contains a
// registered value: an occurrence is either replaced where it completes or
// still held.
//
// A nil StreamRedactor passes text through untouched, which is what Stream
// returns for a Redactor with no exact values, so a run with no Secrets pays
// nothing. It is not safe for concurrent use.
type StreamRedactor struct {
	exact []string
	// starts marks the bytes some value begins with, so a position that cannot
	// start one is passed over without comparing against every value.
	starts  [256]bool
	pending string
}

// Stream returns a StreamRedactor over r's exact values, or nil when there are
// none.
func (r *Redactor) Stream() *StreamRedactor {
	if r == nil || len(r.exact) == 0 {
		return nil
	}
	s := &StreamRedactor{exact: r.exact}
	for _, v := range r.exact {
		s.starts[v[0]] = true
	}
	return s
}

// Write takes the next piece of the stream and returns the redacted text that
// is now safe to emit, which may be empty while a possible value is held.
func (s *StreamRedactor) Write(piece string) string {
	if s == nil {
		return piece
	}
	out, rest := s.scan(s.pending+piece, false)
	s.pending = rest
	return out
}

// Flush returns the held tail, redacted, and resets the redactor. Call it where
// the stream ends -- the end of one model call -- so a turn's last words are
// not withheld while the tools it asked for run.
func (s *StreamRedactor) Flush() string {
	if s == nil || s.pending == "" {
		return ""
	}
	out, _ := s.scan(s.pending, true)
	s.pending = ""
	return out
}

// scan redacts buf from the left: at the first position where buf ends partway
// through a value, scanning stops and the rest is returned to be held; a value
// that matches at a position is replaced (the longest, when several do).
// Unless final, a trailing incomplete UTF-8 character is held too, so no
// emitted piece splits a character. Each decision is one more text cannot
// reverse, so the joined result does not depend on where the pieces split.
func (s *StreamRedactor) scan(buf string, final bool) (out, rest string) {
	var b strings.Builder
	lit := 0 // start of the literal run not yet written
	for i := 0; i < len(buf); {
		if !s.starts[buf[i]] {
			i++
			continue
		}
		tail := buf[i:]
		// Holding comes first: a shorter value can match here while a longer
		// one it prefixes is still arriving, and redacting the shorter now
		// would emit the rest of the longer one as plain text.
		if !final && s.couldBecomeValue(tail) {
			b.WriteString(buf[lit:i])
			return b.String(), tail
		}
		if n := s.longestMatch(tail); n > 0 {
			b.WriteString(buf[lit:i])
			b.WriteString(exactMarker)
			i += n
			lit = i
			continue
		}
		i++
	}
	end := len(buf)
	if !final {
		end = incompleteRuneStart(buf, lit)
	}
	b.WriteString(buf[lit:end])
	return b.String(), buf[end:]
}

// longestMatch is the length of the longest value tail starts with, or zero.
func (s *StreamRedactor) longestMatch(tail string) int {
	n := 0
	for _, v := range s.exact {
		if len(v) > n && strings.HasPrefix(tail, v) {
			n = len(v)
		}
	}
	return n
}

// couldBecomeValue reports whether tail is a proper prefix of some value, so
// the next piece could complete it.
func (s *StreamRedactor) couldBecomeValue(tail string) bool {
	for _, v := range s.exact {
		if len(tail) < len(v) && strings.HasPrefix(v, tail) {
			return true
		}
	}
	return false
}

// incompleteRuneStart is where a trailing incomplete UTF-8 character in
// buf[from:] begins, or len(buf) when buf ends on a character boundary.
func incompleteRuneStart(buf string, from int) int {
	for j := len(buf) - 1; j >= from && j > len(buf)-utf8.UTFMax; j-- {
		if utf8.RuneStart(buf[j]) {
			if !utf8.FullRuneInString(buf[j:]) {
				return j
			}
			break
		}
	}
	return len(buf)
}
