package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/llmwire"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/server/authtoken"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
)

// workerRunClaims is what the scheduler would have minted for the run the tests
// below execute.
func workerRunClaims() authtoken.RunClaims {
	return authtoken.RunClaims{
		UserID:    llmTestUser,
		SpaceID:   llmTestSpace,
		TaskRunID: "r_1",
		TaskID:    "t_1",
	}
}

func workerRunToken(t *testing.T, claims authtoken.RunClaims, ttl time.Duration, issuedAt time.Time) string {
	t.Helper()
	token, err := authtoken.MintRun(workerTestSecret, claims, ttl, issuedAt)
	if err != nil {
		t.Fatalf("MintRun: %v", err)
	}
	return token
}

func validWorkerRunToken(t *testing.T) string {
	t.Helper()
	return workerRunToken(t, workerRunClaims(), time.Hour, time.Now())
}

func workerLLMHandler(gateway *llmgateway.Service, runStatus string) http.Handler {
	h := New(Config{
		JWTSecret: workerTestSecret,
		Gateway:   gateway,
		TaskRuns: &mock.MockTaskRunStore{
			Runs:     []coretask.Run{{ID: "r_1", TaskID: "t_1", Status: runStatus, CreatedAt: time.Unix(1, 0).UTC()}},
			TaskList: []coretask.Task{{ID: "t_1", ConversationID: "c_1", SpaceID: llmTestSpace, Status: runStatus, Input: "in", CreatedBy: llmTestUser, CreatedAt: time.Unix(1, 0).UTC()}},
		},
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

// workerLLMRequest issues a worker-route completion for a run in the given
// status, authenticated with token.
func workerLLMRequest(t *testing.T, gateway *llmgateway.Service, runStatus, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/worker/task-runs/r_1/llm/completions", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	workerLLMHandler(gateway, runStatus).ServeHTTP(rec, req)
	return rec
}

const workerLLMBody = `{"model":"Fast","messages":[{"role":"user","content":"hi"}]}`

func TestWorkerLLMCompletions(t *testing.T) {
	gateway := llmTestService(t, &llmStubClient{content: "answer"}, nil)

	t.Run("answers a run that is executing", func(t *testing.T) {
		rec := workerLLMRequest(t, gateway, string(coretask.RunStatusRunning), workerLLMBody, validWorkerRunToken(t))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "answer") {
			t.Errorf("body %q should carry the completion", rec.Body.String())
		}
	})

	// A run token proves which run is calling, but not that the run is still
	// going. Without the status check a token would keep spending a space's quota
	// against work that finished, right up to its expiry.
	t.Run("refuses a run that is not executing", func(t *testing.T) {
		for _, status := range []coretask.RunStatus{
			coretask.RunStatusPending,
			coretask.RunStatusScheduled,
			coretask.RunStatusSucceeded,
			coretask.RunStatusFailed,
		} {
			rec := workerLLMRequest(t, gateway, string(status), workerLLMBody, validWorkerRunToken(t))
			if rec.Code != http.StatusConflict {
				t.Errorf("status %s: got %d, want 409", status, rec.Code)
			}
		}
	})

	t.Run("refuses a token it cannot verify", func(t *testing.T) {
		tests := []struct {
			name  string
			token string
		}{
			{"no credential at all", ""},
			{"not a token", "not-a-token"},
			{"expired", workerRunToken(t, workerRunClaims(), time.Minute, time.Now().Add(-time.Hour))},
			{"signed by another deployment", func() string {
				other, err := authtoken.MintRun("some-other-secret", workerRunClaims(), time.Hour, time.Now())
				if err != nil {
					t.Fatalf("MintRun: %v", err)
				}
				return other
			}()},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				rec := workerLLMRequest(t, gateway, string(coretask.RunStatusRunning), workerLLMBody, tc.token)
				if rec.Code != http.StatusUnauthorized {
					t.Errorf("status = %d, want 401", rec.Code)
				}
			})
		}
	})

	// The check that makes the credential per-run rather than per-worker. A
	// compromised run holding its own valid token must not be able to bill
	// another run by pointing at that run's URL.
	t.Run("refuses a token minted for another run", func(t *testing.T) {
		claims := workerRunClaims()
		claims.TaskRunID = "r_somebody_else"
		rec := workerLLMRequest(t, gateway, string(coretask.RunStatusRunning), workerLLMBody,
			workerRunToken(t, claims, time.Hour, time.Now()))
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
	})

	// Token and server state must agree. They can only diverge if a run changed
	// hands after dispatch, and the safe reading of that is refusal.
	t.Run("refuses a token whose space is not the run's", func(t *testing.T) {
		claims := workerRunClaims()
		claims.SpaceID = "tm_other"
		rec := workerLLMRequest(t, gateway, string(coretask.RunStatusRunning), workerLLMBody,
			workerRunToken(t, claims, time.Hour, time.Now()))
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
	})

	// A task run belongs to whoever created it. A ledger that recorded only the
	// space could not answer whose work spent the tokens.
	t.Run("attributes the call to the run's user and space", func(t *testing.T) {
		attributed := llmTestService(t, &llmStubClient{content: "answer"}, nil)
		rec := workerLLMRequest(t, attributed, string(coretask.RunStatusRunning), workerLLMBody, validWorkerRunToken(t))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		ledger, ok := attributed.Ledger.(*llmStubLedger)
		if !ok {
			t.Fatalf("unexpected ledger type %T", attributed.Ledger)
		}
		call := ledger.last
		if call.UserID == nil || *call.UserID != llmTestUser {
			t.Errorf("user = %v, want %q", call.UserID, llmTestUser)
		}
		if call.TaskRunID == nil || *call.TaskRunID != "r_1" {
			t.Errorf("task run = %v, want r_1", call.TaskRunID)
		}
		if call.TaskID == nil || *call.TaskID != "t_1" {
			t.Errorf("task = %v, want t_1", call.TaskID)
		}
		if call.Surface != workerSurface {
			t.Errorf("surface = %q, want %q", call.Surface, workerSurface)
		}
	})

	t.Run("never reveals the upstream model, endpoint, or credential", func(t *testing.T) {
		rec := workerLLMRequest(t, gateway, string(coretask.RunStatusRunning), workerLLMBody, validWorkerRunToken(t))
		for _, secret := range []string{"SECRET-ENDPOINT", "SECRET-CREDENTIAL", "SECRET-UPSTREAM-MODEL"} {
			if strings.Contains(rec.Body.String(), secret) {
				t.Errorf("response leaked %q: %s", secret, rec.Body.String())
			}
		}
	})

	// A worker states what it is doing, never who for. Everything the ledger
	// attributes comes from the token, so there is no field here to forge.
	t.Run("rejects a body carrying fields it does not define", func(t *testing.T) {
		rec := workerLLMRequest(t, gateway,
			string(coretask.RunStatusRunning),
			`{"model":"Fast","messages":[{"role":"user","content":"hi"}],"space_id":"tm_other"}`,
			validWorkerRunToken(t))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 for an unknown field", rec.Code)
		}
	})

	t.Run("404s a run the server does not have", func(t *testing.T) {
		h := New(Config{
			JWTSecret: workerTestSecret,
			Gateway:   gateway,
			TaskRuns:  &mock.MockTaskRunStore{},
		})
		mux := http.NewServeMux()
		h.Register(mux)

		claims := workerRunClaims()
		claims.TaskRunID = "r_missing"
		req := httptest.NewRequest(http.MethodPost, "/api/worker/task-runs/r_missing/llm/completions", strings.NewReader(workerLLMBody))
		req.Header.Set("Authorization", "Bearer "+workerRunToken(t, claims, time.Hour, time.Now()))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})
}

// workerDenyQuota refuses every space.
type workerDenyQuota struct{}

func (workerDenyQuota) Check(context.Context, string, int, int) (bool, string, error) {
	return false, "quota exceeded: token limit", nil
}

// TestWorkerLLMCompletionRespectsQuota is where quota is enforced now that a
// foreground call names no space: a run belongs to exactly one, taken from its
// run token, so this is the route that can be metered against a limit.
func TestWorkerLLMCompletionRespectsQuota(t *testing.T) {
	gateway := llmTestService(t, &llmStubClient{content: "answer"}, workerDenyQuota{})

	rec := workerLLMRequest(t, gateway, string(coretask.RunStatusRunning), workerLLMBody, validWorkerRunToken(t))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), llmgateway.ErrorClassQuotaExceeded) {
		t.Errorf("body does not carry the quota code: %s", rec.Body.String())
	}
}

// The worker route resolves a profile the same way the user route does. Two
// endpoints reaching different conclusions about the same word is the drift the
// shared resolver exists to prevent, and here it would mean a run's caching
// depended on which door it came through.
func TestWorkerLLMCompletionCarriesTheCallProfile(t *testing.T) {
	client := &llmStubClient{content: "answer"}
	gateway := llmTestService(t, client, nil)

	rec := workerLLMRequest(t, gateway, string(coretask.RunStatusRunning),
		`{"model":"Fast","messages":[{"role":"user","content":"hi"}],"call_profile":"agent_turn"}`,
		validWorkerRunToken(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if client.gotProfile != cllm.ProfileAgentTurn {
		t.Errorf("provider saw profile %q, want %q", client.gotProfile, cllm.ProfileAgentTurn)
	}
}

// A managed worker run requests its output schema through this route and needs
// the provider client's verdict back. Dropping either half leaves a Workflow
// node with an output_schema unable to succeed on any managed worker.
func TestWorkerLLMCompletionCarriesStructuredOutput(t *testing.T) {
	client := &llmStubClient{content: `{"label":"bug"}`, structured: &cllm.Structured{
		Value: json.RawMessage(`{"label":"bug"}`), Mode: cllm.StructuredNative, Enforced: true,
	}}
	gateway := llmTestService(t, client, nil)

	rec := workerLLMRequest(t, gateway, string(coretask.RunStatusRunning),
		`{"model":"Fast","messages":[{"role":"user","content":"hi"}],"output":{"name":"output","schema":{"type":"object"}}}`,
		validWorkerRunToken(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if client.gotOutput == nil || client.gotOutput.Name != "output" || string(client.gotOutput.Schema) != `{"type":"object"}` {
		t.Errorf("provider saw output %+v, want the requested schema", client.gotOutput)
	}
	var got llmwire.CompletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Structured == nil || string(got.Structured.Value) != `{"label":"bug"}` ||
		got.Structured.Mode != string(cllm.StructuredNative) || !got.Structured.Enforced {
		t.Errorf("structured = %+v, want the provider client's validated value", got.Structured)
	}
}

// An output with no schema is a malformed request, refused before any
// provider call rather than turned into a structured failure after one.
func TestWorkerLLMCompletionRefusesAnOutputWithoutSchema(t *testing.T) {
	client := &llmStubClient{content: "answer"}
	gateway := llmTestService(t, client, nil)

	rec := workerLLMRequest(t, gateway, string(coretask.RunStatusRunning),
		`{"model":"Fast","messages":[{"role":"user","content":"hi"}],"output":{"name":"output"}}`,
		validWorkerRunToken(t))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if client.gotOutput != nil || client.gotProfile != "" {
		t.Error("a refused request still reached the provider")
	}
}

func TestWorkerLLMCompletionRefusesAnUnknownProfile(t *testing.T) {
	client := &llmStubClient{content: "answer"}
	gateway := llmTestService(t, client, nil)

	rec := workerLLMRequest(t, gateway, string(coretask.RunStatusRunning),
		`{"model":"Fast","messages":[{"role":"user","content":"hi"}],"call_profile":"cache_everything"}`,
		validWorkerRunToken(t))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if client.gotProfile != "" {
		t.Error("a refused request still reached the provider")
	}
}

// A worker cannot name a cache policy any more than a CLI can. The wire
// contract has no field for one, and an unknown field is refused rather than
// ignored.
func TestWorkerLLMCompletionRefusesACachePolicy(t *testing.T) {
	client := &llmStubClient{content: "answer"}
	gateway := llmTestService(t, client, nil)

	for _, body := range []string{
		`{"messages":[{"role":"user","content":"hi"}],"cache_control":{"mode":"force"}}`,
		`{"messages":[{"role":"user","content":"hi"}],"cache_ttl":"1h"}`,
	} {
		rec := workerLLMRequest(t, gateway, string(coretask.RunStatusRunning), body, validWorkerRunToken(t))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s produced status %d, want 400", body, rec.Code)
		}
	}
	if client.gotProfile != "" {
		t.Error("a refused request still reached the provider")
	}
}
