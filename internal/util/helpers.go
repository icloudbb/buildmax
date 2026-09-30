package util

import (
	"fmt"
	"os"
	"time"
	"unicode/utf8"
)

// WithEnvVar sets envKey to value for the duration of fn, then restores the previous process env state.
func WithEnvVar(envKey, value string, fn func() error) error {
	prev, hadPrev := os.LookupEnv(envKey)
	if err := os.Setenv(envKey, value); err != nil {
		return err
	}
	defer func() {
		if hadPrev {
			_ = os.Setenv(envKey, prev)
		} else {
			_ = os.Unsetenv(envKey)
		}
	}()
	return fn()
}

// WithEnvVars sets several environment variables for the duration of fn, then
// restores each to its previous state. Like WithEnvVar it mutates process
// environment, so it assumes the caller is not running fn concurrently with
// another that touches the same keys.
func WithEnvVars(vars map[string]string, fn func() error) error {
	type saved struct {
		val string
		had bool
	}
	prev := make(map[string]saved, len(vars))
	for k, v := range vars {
		old, had := os.LookupEnv(k)
		prev[k] = saved{val: old, had: had}
		if err := os.Setenv(k, v); err != nil {
			return err
		}
	}
	defer func() {
		for k, s := range prev {
			if s.had {
				_ = os.Setenv(k, s.val)
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}()
	return fn()
}

// Ptr returns a pointer to v. Useful for filling optional pointer fields.
func Ptr[T any](v T) *T {
	return &v
}

// FormatDuration formats a duration in a compact human-readable way.
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", m, s)
}

// FormatMinute formats an instant as YYYY-MM-DD HH:MM in local time.
//
// The reader is a person — a tool result or a listing — so local time is the
// useful rendering. Stored and transported instants stay UTC.
func FormatMinute(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04")
}

// ExceedsByteLimit reports whether s is longer than maxBytes bytes. A MySQL
// TEXT column is bounded in bytes (65535), so a field that maps to one is
// checked in bytes: the cap set to the column capacity refuses only input that
// could not be stored anyway, turning a write error into a clean rejection.
func ExceedsByteLimit(s string, maxBytes int) bool {
	return len(s) > maxBytes
}

// ExceedsRuneLimit reports whether s is longer than maxRunes runes. It counts
// runes, not bytes, because a MySQL varchar(N) bounds characters — so a
// validation that mirrors a column cap has to count the same way, or a
// multi-byte string that fits the column is wrongly refused (or one that does
// not is wrongly stored and errors at write time).
func ExceedsRuneLimit(s string, maxRunes int) bool {
	return utf8.RuneCountInString(s) > maxRunes
}

// TruncateRunes truncates s to at most maxRunes runes and appends an ellipsis
// when truncation happens. If maxRunes is non-positive, it returns the empty string.
func TruncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "…"
}

// ClipRunes returns at most the first maxRunes runes of s without adding a suffix.
// If maxRunes is non-positive, it returns the empty string.
func ClipRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes])
}
