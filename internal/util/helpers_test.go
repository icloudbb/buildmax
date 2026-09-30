package util

import (
	"os"
	"testing"
	"time"
)

func TestWithEnvVar_RestoresExistingValue(t *testing.T) {
	const key = "BUILDMAX_TEST_ENV_RESTORE_EXISTING"
	if err := os.Setenv(key, "before"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Unsetenv(key) }()

	if err := WithEnvVar(key, "during", func() error {
		got := os.Getenv(key)
		if got != "during" {
			t.Fatalf("inside WithEnvVar: got %q, want during", got)
		}
		return nil
	}); err != nil {
		t.Fatalf("WithEnvVar: %v", err)
	}

	if got := os.Getenv(key); got != "before" {
		t.Fatalf("after WithEnvVar: got %q, want before", got)
	}
}

func TestWithEnvVar_RestoresUnsetState(t *testing.T) {
	const key = "BUILDMAX_TEST_ENV_RESTORE_UNSET"
	_ = os.Unsetenv(key)

	if err := WithEnvVar(key, "during", func() error {
		got := os.Getenv(key)
		if got != "during" {
			t.Fatalf("inside WithEnvVar: got %q, want during", got)
		}
		return nil
	}); err != nil {
		t.Fatalf("WithEnvVar: %v", err)
	}

	if _, ok := os.LookupEnv(key); ok {
		t.Fatalf("after WithEnvVar: %s should be unset", key)
	}
}

func TestWithEnvVars_SetsAndRestoresEach(t *testing.T) {
	const set = "BUILDMAX_TEST_ENVS_SET"
	const unset = "BUILDMAX_TEST_ENVS_UNSET"
	t.Setenv(set, "before")
	_ = os.Unsetenv(unset)

	if err := WithEnvVars(map[string]string{set: "during", unset: "during"}, func() error {
		if got := os.Getenv(set); got != "during" {
			t.Fatalf("inside: %s = %q, want during", set, got)
		}
		if got := os.Getenv(unset); got != "during" {
			t.Fatalf("inside: %s = %q, want during", unset, got)
		}
		return nil
	}); err != nil {
		t.Fatalf("WithEnvVars: %v", err)
	}

	if got := os.Getenv(set); got != "before" {
		t.Fatalf("after: %s = %q, want before", set, got)
	}
	if _, ok := os.LookupEnv(unset); ok {
		t.Fatalf("after: %s should be unset again", unset)
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxRunes int
		want     string
	}{
		{name: "non_positive_limit", input: "hello", maxRunes: 0, want: ""},
		{name: "shorter_than_limit", input: "hello", maxRunes: 10, want: "hello"},
		{name: "equal_to_limit", input: "hello", maxRunes: 5, want: "hello"},
		{name: "truncate_ascii", input: "hello world", maxRunes: 5, want: "hello…"},
		{name: "truncate_unicode", input: "你好世界再见", maxRunes: 4, want: "你好世界…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateRunes(tt.input, tt.maxRunes); got != tt.want {
				t.Fatalf("TruncateRunes(%q, %d) = %q, want %q", tt.input, tt.maxRunes, got, tt.want)
			}
		})
	}
}

func TestPtr(t *testing.T) {
	s := Ptr("hello")
	if s == nil || *s != "hello" {
		t.Fatalf("Ptr(string) = %v, want pointer to hello", s)
	}

	n := Ptr(int32(3))
	if n == nil || *n != 3 {
		t.Fatalf("Ptr(int32) = %v, want pointer to 3", n)
	}
}

func TestClipRunes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxRunes int
		want     string
	}{
		{name: "non_positive_limit", input: "hello", maxRunes: 0, want: ""},
		{name: "shorter_than_limit", input: "hello", maxRunes: 10, want: "hello"},
		{name: "equal_to_limit", input: "hello", maxRunes: 5, want: "hello"},
		{name: "clip_ascii", input: "hello world", maxRunes: 5, want: "hello"},
		{name: "clip_unicode", input: "你好世界再见", maxRunes: 4, want: "你好世界"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClipRunes(tt.input, tt.maxRunes); got != tt.want {
				t.Fatalf("ClipRunes(%q, %d) = %q, want %q", tt.input, tt.maxRunes, got, tt.want)
			}
		})
	}
}

func TestFormatMinute(t *testing.T) {
	// FormatMinute renders in the process's local zone, which is intentional:
	// the result is shown to users. Pin the zone so the expectation does not
	// depend on where the test runs — it previously only held in UTC+8.
	orig := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = orig })

	got := FormatMinute(time.Unix(1700000000, 0))
	if want := "2023-11-14 22:13"; got != want {
		t.Fatalf("FormatMinute() = %q, want %q", got, want)
	}
}

func TestExceedsRuneLimit(t *testing.T) {
	if ExceedsRuneLimit("abc", 3) {
		t.Error("3 runes should fit a limit of 3")
	}
	if !ExceedsRuneLimit("abcd", 3) {
		t.Error("4 runes should exceed a limit of 3")
	}
	// Counts runes, not bytes: 3 multi-byte characters fit a limit of 3.
	if ExceedsRuneLimit("日本語", 3) {
		t.Error("3 multi-byte runes should fit a limit of 3")
	}
	if !ExceedsRuneLimit("日本語", 2) {
		t.Error("3 runes should exceed a limit of 2")
	}
}

func TestExceedsByteLimit(t *testing.T) {
	if ExceedsByteLimit("abc", 3) {
		t.Error("3 bytes should fit a limit of 3")
	}
	if !ExceedsByteLimit("abcd", 3) {
		t.Error("4 bytes should exceed a limit of 3")
	}
	// Counts bytes: one 3-byte rune exceeds a 2-byte limit.
	if !ExceedsByteLimit("日", 2) {
		t.Error("a 3-byte rune should exceed a 2-byte limit")
	}
}
