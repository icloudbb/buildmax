package workerclient

import (
	"net/url"
	"strings"
)

// serverAddressPlaceholder replaces the worker API's address in text a person
// or a model reads.
const serverAddressPlaceholder = "[worker API]"

// redactServerAddress removes the worker API's address from msg.
//
// That address is where a worker reaches its server — in a cluster an internal
// service name — so it opens nothing for the person reading a run's error or
// the model reading a tool result, and a model repeats what it reads into
// output other people and later steps act on. The host is removed on its own
// too, because a transport error names it without the scheme ("lookup <host>"
// or "dial tcp <host>:<port>"). The worker's log keeps the address.
func redactServerAddress(msg, baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if msg == "" || baseURL == "" {
		return msg
	}
	msg = strings.ReplaceAll(msg, baseURL, serverAddressPlaceholder)
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return msg
	}
	msg = strings.ReplaceAll(msg, u.Host, serverAddressPlaceholder)
	if host := u.Hostname(); host != u.Host {
		msg = strings.ReplaceAll(msg, host, serverAddressPlaceholder)
	}
	return msg
}

// redactedError is an error whose text no longer names the worker API's
// address. It unwraps to the original, so errors.Is and errors.As still see
// the cause.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// hideServerAddress returns err with the worker API's address removed from its
// text, or err itself when the text did not contain it.
func hideServerAddress(err error, baseURL string) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	redacted := redactServerAddress(text, baseURL)
	if redacted == text {
		return err
	}
	return &redactedError{msg: redacted, err: err}
}
