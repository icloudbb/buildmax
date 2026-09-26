// Package httpclient is the client-side plumbing shared by calls to a BuildMax
// server: decoding its error envelope, streaming an upload, and renewing a
// bearer token the server refused.
//
// Every refusal outside the managed gateway comes back from
// httputil.WriteJSONError as {"error": "..."}, so a client that reports only
// resp.Status throws away the one sentence saying what went wrong. This is the
// one reader of that envelope, so no client has to remember to look.
//
// The gateway is deliberately not a caller of the decoder: it answers llmwire.ErrorResponse, a
// versioned contract with its own code field, and internal/infra/llmremote
// decodes it against that contract rather than this one.
package httpclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// maxErrorBody caps what a failing response can make a client read. The
// envelope is one short sentence; anything longer is a proxy's HTML error page.
const maxErrorBody = 4096

// Error is a refused HTTP request. Message is what the server said, empty when
// it said nothing decodable.
type Error struct {
	StatusCode int
	Message    string
	// Op names the call, e.g. "worker API GET /api/worker/task-runs/r_1", so
	// the error reads usefully where it surfaces rather than only where it was
	// created.
	Op string
}

func (e *Error) Error() string {
	switch {
	case e.Op != "" && e.Message != "":
		return fmt.Sprintf("%s: %s (%d)", e.Op, e.Message, e.StatusCode)
	case e.Op != "":
		return fmt.Sprintf("%s: %s", e.Op, http.StatusText(e.StatusCode))
	case e.Message != "":
		return fmt.Sprintf("server %d: %s", e.StatusCode, e.Message)
	default:
		return fmt.Sprintf("server returned %d", e.StatusCode)
	}
}

// DecodeError reads the envelope out of a refused response.
//
// It consumes the body, so callers use it only on a status they have already
// decided is a failure. "message" is accepted alongside "error" because a proxy
// in front of the server may answer in its own shape.
func DecodeError(resp *http.Response, op string) *Error {
	out := &Error{StatusCode: resp.StatusCode, Op: op}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err != nil || len(data) == 0 {
		return out
	}
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &body) != nil {
		return out
	}
	out.Message = body.Error
	if out.Message == "" {
		out.Message = body.Message
	}
	return out
}
