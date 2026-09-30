package secret

import "testing"

// Destruction is terminal, and a state to itself is a harmless no-op; every
// other move among active/disabled is allowed.
func TestValidStateTransition(t *testing.T) {
	cases := []struct {
		from, to State
		want     bool
	}{
		{StateActive, StateActive, true},
		{StateActive, StateDisabled, true},
		{StateActive, StateDestroyed, true},
		{StateDisabled, StateActive, true},
		{StateDisabled, StateDisabled, true},
		{StateDisabled, StateDestroyed, true},
		{StateDestroyed, StateDestroyed, true}, // no-op re-destroy
		{StateDestroyed, StateActive, false},   // terminal
		{StateDestroyed, StateDisabled, false}, // terminal
	}
	for _, tc := range cases {
		if got := ValidStateTransition(tc.from, tc.to); got != tc.want {
			t.Errorf("ValidStateTransition(%s, %s) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestIsItemName(t *testing.T) {
	valid := []string{"TOKEN", "api_key", "_x", "A1_b2"}
	invalid := []string{"", "1abc", "has-dash", "has space", "dot.name", "€uro"}
	for _, s := range valid {
		if !IsItemName(s) {
			t.Errorf("IsItemName(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if IsItemName(s) {
			t.Errorf("IsItemName(%q) = true, want false", s)
		}
	}
}
