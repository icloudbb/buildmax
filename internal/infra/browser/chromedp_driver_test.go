package browser

import (
	"strings"
	"testing"
)

// A failed launch carries Chrome's own last words, bounded, and names the
// sandbox cause that Chrome's wording alone does not explain.
func TestOutputTailExplainsALaunchFailure(t *testing.T) {
	o := &outputTail{}
	_, _ = o.Write([]byte(strings.Repeat("noise ", 1000)))
	_, _ = o.Write([]byte("FATAL: No usable sandbox! see https://example.test"))
	got := o.explain()
	if !strings.Contains(got, "No usable sandbox") || !strings.Contains(got, "apparmor_restrict_unprivileged_userns") {
		t.Errorf("explain() = %q", got)
	}
	if len(got) > outputTailBytes+400 {
		t.Errorf("explain() is %d bytes; the tail is not bounded", len(got))
	}
	if (&outputTail{}).explain() != "" {
		t.Error("an empty tail produced text")
	}
}
