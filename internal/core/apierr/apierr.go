// Package apierr is the vocabulary a service uses to say why it refused.
//
// A service knows the reason -- the thing is missing, the input is wrong, the
// caller may not -- and nothing about HTTP. A transport knows how to answer but
// would otherwise have to recognise each service's sentinels one by one, which
// is how four hand-written switches over seventy-odd errors came about. A Kind
// is the join: the service names it, the transport maps it once.
//
// The pattern is internal/service/llmgateway's, generalised. It classified its
// own errors into stable classes and mapped class to status in one place; this
// is the same idea with the class attached to the sentinel instead of derived
// from it, so nothing has to be kept in step.
package apierr

import (
	"errors"
	"fmt"
)

// Kind is why a call was refused.
type Kind string

const (
	// KindNotConfigured means the deployment has no such capability wired up --
	// a nil store on a server running without a database. It is not the
	// caller's fault and retrying the same call will not help.
	KindNotConfigured Kind = "not_configured"
	// KindInvalid means the request said something wrong. This covers a field
	// naming a thing that does not exist: the request is the problem, not the
	// path.
	KindInvalid Kind = "invalid"
	// KindNotFound means the addressed resource does not exist.
	KindNotFound Kind = "not_found"
	// KindForbidden means it exists and the caller may not do this to it.
	KindForbidden Kind = "forbidden"
	// KindConflict means the current state refuses the change.
	KindConflict Kind = "conflict"
	// KindQuotaExceeded means the space is over an allowance.
	KindQuotaExceeded Kind = "quota_exceeded"
	// KindUnavailable means a dependency the server needs -- object storage,
	// the coordination backend -- did not answer. Not the caller's fault, and
	// unlike KindNotConfigured, retrying shortly may succeed.
	KindUnavailable Kind = "unavailable"
)

// Error carries a Kind and the sentence the caller is told.
//
// The message is the answer, not an internal note: a transport writes it
// verbatim, so it must not name anything the caller should not learn. Detail
// adds to it only where the extra text is deliberately public.
type Error struct {
	kind    Kind
	message string
	base    error
}

func (e *Error) Error() string { return e.message }
func (e *Error) Kind() Kind    { return e.kind }

// Unwrap reports the sentinel a detailed error was built from, so errors.Is
// still matches it.
func (e *Error) Unwrap() error { return e.base }

// New declares a sentinel. message is what the caller is told.
func New(kind Kind, message string) *Error {
	return &Error{kind: kind, message: message}
}

// Detail returns a copy of base whose message carries extra text and which
// still matches errors.Is(base).
//
// Opting in per call site is the point: a parse error is worth telling the
// caller, and most things a service knows are not.
func Detail(base *Error, format string, args ...any) *Error {
	return &Error{
		kind:    base.kind,
		message: base.message + ": " + fmt.Sprintf(format, args...),
		base:    base,
	}
}

// KindOf reports the Kind carried anywhere in err's chain.
func KindOf(err error) (Kind, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.kind, true
	}
	return "", false
}

// Message reports the caller-facing sentence carried in err's chain.
func Message(err error) (string, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.message, true
	}
	return "", false
}
