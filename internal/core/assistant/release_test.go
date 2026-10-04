package assistant

import (
	"strings"
	"testing"
)

func TestReleaseKeepsOnlyContractedFields(t *testing.T) {
	entry := &RosterEntry{Kind: KindAgent, ID: "a", Releasable: []string{"days", "note", "missing"}}
	str := func(s string) *string { return &s }

	got := FormatReleased(Release(str(`{"raw":"secret","note":"ask HR","days":15,"nested":{"x":1}}`), entry))
	if got != "days: 15\nnote: ask HR" {
		t.Errorf("released = %q", got)
	}
	for name, result := range map[string]*string{
		"nil result":    nil,
		"not JSON":      str("SECRET free text"),
		"not object":    str(`["SECRET"]`),
		"no contracted": str(`{"raw":"SECRET"}`),
	} {
		if fields := Release(result, entry); len(fields) != 0 {
			t.Errorf("%s released %+v", name, fields)
		}
	}
	if fields := Release(str(`{"days":1}`), nil); fields != nil {
		t.Errorf("no roster entry released %+v", fields)
	}
	long := FormatReleased(Release(str(`{"note":"`+strings.Repeat("x", 5000)+`"}`), entry))
	if n := len([]rune(long)); n > maxReleasedRunes+1 {
		t.Errorf("released %d runes", n)
	}
}

func TestEntryFindsByKindAndID(t *testing.T) {
	d := Definition{Roster: []RosterEntry{{Kind: KindAgent, ID: "x"}, {Kind: KindWorkflow, ID: "x", Releasable: []string{"r"}}}}
	if e := d.Entry(KindWorkflow, "x"); e == nil || len(e.Releasable) != 1 {
		t.Errorf("Entry = %+v", e)
	}
	if d.Entry(KindAgent, "y") != nil {
		t.Error("found an entry the roster lacks")
	}
}
