package task

import (
	"context"
	"testing"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/mock"
)

// Without a title model, a Task run on an Agent with no instruction of its
// own is titled by the Agent, never by the first line of the Agent definition
// that became its input ("Agent: … Description: …").
func TestCreateTaskFallbackTitleNamesTheAgentForAComposedInput(t *testing.T) {
	agentID := "a_1"
	agents := &mock.MockAgentStore{Agents: []agentdef.Agent{{
		ID: agentID, SpaceID: "tm_1", Name: "QA Reviewer", Description: "Reviews acceptance criteria.", Instructions: "Review it.",
	}}}
	taskStore := &mock.MockTaskStore{}
	svc := &Service{Agents: agents, Tasks: taskStore}

	for _, tc := range []struct{ input, want string }{
		{input: "", want: "QA Reviewer"},
		{input: "Check the release notes", want: "Check the release notes"},
	} {
		taskStore.Created = nil
		if _, err := svc.CreateTask(context.Background(), CreateTaskCmd{UserID: "u1", SpaceID: "tm_1", Input: tc.input, AgentID: &agentID}); err != nil {
			t.Fatalf("CreateTask(%q): %v", tc.input, err)
		}
		if got := taskStore.Created[0].Title; got != tc.want {
			t.Errorf("CreateTask(%q) title = %q, want %q", tc.input, got, tc.want)
		}
	}
}
