package httputil

import (
	"net/http"

	"github.com/icloudbb/buildmax/internal/core/apierr"
)

// WriteServiceError answers a service error and reports whether it did.
//
// It replaces the per-service switches that each recognised one package's
// sentinels: a service states a Kind, this maps Kind to status once. False
// means the error carries no Kind, which is the caller's signal to treat it as
// an internal failure -- an unclassified error is a bug, not a refusal.
func WriteServiceError(w http.ResponseWriter, err error) bool {
	kind, ok := apierr.KindOf(err)
	if !ok {
		return false
	}
	message, _ := apierr.Message(err)
	// Deliberately the same body as every other refusal, {"error": ...}. The
	// Kind is a code this could also report, but adding one is an API decision,
	// not a side effect of moving the switch.
	WriteJSONError(w, statusForKind(kind), message)
	return true
}

func statusForKind(kind apierr.Kind) int {
	switch kind {
	case apierr.KindNotConfigured, apierr.KindUnavailable:
		return http.StatusServiceUnavailable
	case apierr.KindInvalid:
		return http.StatusBadRequest
	case apierr.KindNotFound:
		return http.StatusNotFound
	case apierr.KindForbidden:
		return http.StatusForbidden
	case apierr.KindConflict:
		return http.StatusConflict
	case apierr.KindQuotaExceeded:
		return http.StatusTooManyRequests
	default:
		// A Kind nobody mapped is a Kind nobody thought about. Saying 500 is
		// honest; guessing 400 would blame the caller for our omission.
		return http.StatusInternalServerError
	}
}
