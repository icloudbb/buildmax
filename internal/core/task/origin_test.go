package task

import (
	"reflect"
	"testing"
)

func TestOriginRelationsOrigin(t *testing.T) {
	cases := []struct {
		name string
		in   OriginRelations
		want *Origin
	}{
		{name: "no relation", in: OriginRelations{}, want: nil},
		{
			name: "a Workflow step outranks the Issue its Task carries",
			in:   OriginRelations{WorkflowRunID: "wr_1", WorkflowName: "QA review", WorkflowStep: 2, IssueID: "iss_1", IssueTitle: "Fix imports"},
			want: &Origin{Kind: OriginWorkflow, ID: "wr_1", Name: "QA review", Step: 2},
		},
		{
			name: "an Issue outranks a Conversation and a Schedule",
			in:   OriginRelations{IssueID: "iss_1", IssueTitle: "Fix imports", ConversationID: "c_1", ScheduleID: "s_1"},
			want: &Origin{Kind: OriginIssue, ID: "iss_1", Name: "Fix imports"},
		},
		{
			name: "a Conversation",
			in:   OriginRelations{ConversationID: "c_1", ConversationTitle: "Release chat"},
			want: &Origin{Kind: OriginConversation, ID: "c_1", Name: "Release chat"},
		},
		{
			name: "a Schedule",
			in:   OriginRelations{ScheduleID: "s_1", ScheduleName: "Nightly digest"},
			want: &Origin{Kind: OriginSchedule, ID: "s_1", Name: "Nightly digest"},
		},
		{
			name: "an origin with no name keeps its kind and ID",
			in:   OriginRelations{IssueID: "iss_1"},
			want: &Origin{Kind: OriginIssue, ID: "iss_1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Origin(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Origin() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
