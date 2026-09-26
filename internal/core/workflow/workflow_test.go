package workflow

import "testing"

func TestValidRunStatusTransition(t *testing.T) {
	allowed := map[RunStatus][]RunStatus{
		RunStatusPending:   {RunStatusRunning, RunStatusFailed, RunStatusCanceled},
		RunStatusRunning:   {RunStatusSucceeded, RunStatusFailed, RunStatusCanceled, RunStatusFailing, RunStatusCanceling},
		RunStatusFailing:   {RunStatusFailed},
		RunStatusCanceling: {RunStatusCanceled},
	}
	all := []RunStatus{RunStatusPending, RunStatusRunning, RunStatusFailing, RunStatusCanceling, RunStatusSucceeded, RunStatusFailed, RunStatusCanceled}
	for _, from := range all {
		ok := make(map[RunStatus]bool)
		for _, to := range allowed[from] {
			ok[to] = true
			if !ValidRunStatusTransition(from, to) {
				t.Errorf("%s -> %s should be allowed", from, to)
			}
		}
		for _, to := range all {
			if !ok[to] && ValidRunStatusTransition(from, to) {
				t.Errorf("%s -> %s should be refused", from, to)
			}
		}
	}
	// Terminal statuses never move, including to themselves.
	for _, from := range []RunStatus{RunStatusSucceeded, RunStatusFailed, RunStatusCanceled} {
		if ValidRunStatusTransition(from, from) {
			t.Errorf("terminal %s must not transition to itself", from)
		}
	}
}

func TestValidNodeRunTransition(t *testing.T) {
	allowed := map[NodeRunStatus][]NodeRunStatus{
		NodeRunStatusPending: {NodeRunStatusRunning, NodeRunStatusBlocked, NodeRunStatusFailed, NodeRunStatusCanceled},
		NodeRunStatusRunning: {NodeRunStatusSucceeded, NodeRunStatusFailed, NodeRunStatusCanceled},
	}
	all := []NodeRunStatus{
		NodeRunStatusPending, NodeRunStatusRunning, NodeRunStatusSucceeded,
		NodeRunStatusFailed, NodeRunStatusCanceled, NodeRunStatusBlocked,
	}
	for _, from := range all {
		ok := make(map[NodeRunStatus]bool)
		for _, to := range allowed[from] {
			ok[to] = true
			if !ValidNodeRunTransition(from, to) {
				t.Errorf("%s -> %s should be allowed", from, to)
			}
		}
		for _, to := range all {
			if !ok[to] && ValidNodeRunTransition(from, to) {
				t.Errorf("%s -> %s should be refused", from, to)
			}
		}
	}
}

func TestStatusTerminal(t *testing.T) {
	if RunStatusTerminal(RunStatusPending) || RunStatusTerminal(RunStatusRunning) || RunStatusTerminal(RunStatusFailing) || RunStatusTerminal(RunStatusCanceling) {
		t.Error("pending/running runs are not terminal")
	}
	for _, s := range []RunStatus{RunStatusSucceeded, RunStatusFailed, RunStatusCanceled} {
		if !RunStatusTerminal(s) {
			t.Errorf("%s run should be terminal", s)
		}
	}
	if NodeRunStatusTerminal(NodeRunStatusPending) || NodeRunStatusTerminal(NodeRunStatusRunning) {
		t.Error("pending/running steps are not terminal")
	}
	// Blocked is terminal alongside the natural ends.
	for _, s := range []NodeRunStatus{NodeRunStatusSucceeded, NodeRunStatusFailed, NodeRunStatusCanceled, NodeRunStatusBlocked} {
		if !NodeRunStatusTerminal(s) {
			t.Errorf("%s step should be terminal", s)
		}
	}
}
