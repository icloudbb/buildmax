package apierr

import (
	"errors"
	"fmt"
	"testing"
)

var errNotFound = New(KindNotFound, "workflow not found")

func TestSentinelCarriesItsKind(t *testing.T) {
	kind, ok := KindOf(errNotFound)
	if !ok || kind != KindNotFound {
		t.Fatalf("KindOf = %q, %v", kind, ok)
	}
	if errNotFound.Error() != "workflow not found" {
		t.Errorf("Error() = %q", errNotFound.Error())
	}
}

// A service wraps for its own context; the kind has to survive that or the
// transport falls back to 500 for an error it could have answered.
func TestKindSurvivesWrapping(t *testing.T) {
	err := fmt.Errorf("load the workflow behind this run: %w", errNotFound)

	kind, ok := KindOf(err)
	if !ok || kind != KindNotFound {
		t.Fatalf("KindOf through a wrap = %q, %v", kind, ok)
	}
	if !errors.Is(err, errNotFound) {
		t.Error("errors.Is should still match the sentinel")
	}
}

// The caller-facing message is the sentinel's, not the wrapper's: a service
// adding "load the workflow behind this run" is writing a log line, not an
// answer.
func TestMessageIgnoresWrapperText(t *testing.T) {
	err := fmt.Errorf("load the workflow behind this run: %w", errNotFound)

	msg, ok := Message(err)
	if !ok || msg != "workflow not found" {
		t.Errorf("Message = %q, %v", msg, ok)
	}
}

func TestDetailAddsToTheMessageAndStillMatches(t *testing.T) {
	base := New(KindInvalid, "invalid workflow definition")

	err := Detail(base, "%v", errors.New("unexpected end of JSON input"))

	if want := "invalid workflow definition: unexpected end of JSON input"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, base) {
		t.Error("a detailed error must still match its sentinel")
	}
	if kind, _ := KindOf(err); kind != KindInvalid {
		t.Errorf("Kind = %q, want the base kind", kind)
	}
	if msg, _ := Message(err); msg != err.Error() {
		t.Errorf("Message = %q, want the detailed text", msg)
	}
}

func TestUnclassifiedErrorReportsNothing(t *testing.T) {
	if _, ok := KindOf(errors.New("boom")); ok {
		t.Error("a plain error carries no kind")
	}
	if _, ok := KindOf(nil); ok {
		t.Error("nil carries no kind")
	}
}

// Two sentinels with the same text are still different errors.
func TestSentinelsAreDistinct(t *testing.T) {
	a := New(KindNotFound, "not found")
	b := New(KindNotFound, "not found")

	if errors.Is(a, b) {
		t.Error("separately declared sentinels must not match each other")
	}
}

// A coded sentinel keeps its code through wrapping and Detail, because the
// client's wording depends on it, not on the message.
func TestCodeSurvivesWrappingAndDetail(t *testing.T) {
	base := NewCoded(KindInvalid, "invitee_account_required", "no account exists for this email")

	if got := CodeOf(fmt.Errorf("invite: %w", base)); got != "invitee_account_required" {
		t.Errorf("CodeOf through a wrap = %q", got)
	}
	if got := CodeOf(Detail(base, "%s", "extra")); got != "invitee_account_required" {
		t.Errorf("CodeOf through Detail = %q", got)
	}
	if got := CodeOf(errNotFound); got != "" {
		t.Errorf("an uncoded sentinel reports code %q", got)
	}
	if got := CodeOf(errors.New("boom")); got != "" {
		t.Errorf("a plain error reports code %q", got)
	}
}
