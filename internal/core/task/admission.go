package task

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// AdmissionFingerprint is a stable digest of the execution-authority fields a
// task admission carries under an AdmissionKey. AdmitTask stores it beside the
// key so a replay of the same logical admission is recognised and returned,
// while a different payload under the same key is rejected as a conflict.
//
// It covers only the caller's intent: input, agent, issue, schedule,
// conversation, creator, and provenance. It deliberately excludes fields the
// task service resolves live from current state — the agent revision and
// sandbox tiers — because a Workflow node dispatch is replayed by re-deriving
// that input, and the linear precursor does not yet pin the agent revision.
// Including a live-resolved value would turn an ordinary recovery after an
// agent edit into a false conflict. Pinning those values is a later slice; when
// it lands, the pinned revision becomes part of the intent this covers.
func AdmissionFingerprint(in *CreateInput) string {
	if in == nil {
		return ""
	}
	// A struct with fixed field order makes the JSON deterministic. Every field
	// is one a differing value must make a different admission.
	payload := struct {
		Space         string  `json:"space"`
		Conversation  string  `json:"conversation"`
		Input         string  `json:"input"`
		Agent         *string `json:"agent"`
		Issue         *string `json:"issue"`
		Schedule      *string `json:"schedule"`
		CreatedBy     string  `json:"created_by"`
		RunCreatedBy  string  `json:"run_created_by"`
		CreatedByType string  `json:"created_by_type"`
		TriggerSource string  `json:"trigger_source"`
		SourceMessage *string `json:"source_message"`
		OutputSchema  *string `json:"output_schema"`
	}{
		Space:         in.SpaceID,
		Conversation:  in.ConversationID,
		Input:         in.Input,
		Agent:         in.AgentID,
		Issue:         in.IssueID,
		Schedule:      in.ScheduleID,
		CreatedBy:     in.CreatedBy,
		RunCreatedBy:  in.InitialRunCreatedBy,
		CreatedByType: in.InitialRunCreatedByType,
		TriggerSource: in.InitialRunTriggerSource,
		SourceMessage: in.InitialRunSourceMessageID,
		OutputSchema:  in.OutputSchema,
	}
	// json.Marshal cannot fail for this fixed shape of strings and string
	// pointers; the error is ignored because there is no value it could take.
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
