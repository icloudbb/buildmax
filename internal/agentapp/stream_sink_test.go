package agentapp

import (
	"reflect"
	"testing"
)

type markingSink struct{ got *[]string }

func (m markingSink) OnDelta(d string) { *m.got = append(*m.got, d) }
func (m markingSink) OnStreamEnd()     { *m.got = append(*m.got, "<end>") }

type plainSink struct{ got *[]string }

func (p plainSink) OnDelta(d string) { *p.got = append(*p.got, d) }

// The Remote Control tee must pass the end of a model call on to a leg that
// holds text back, or a worker's held tail would wait for the next turn.
func TestTeeStreamSinkForwardsStreamEnd(t *testing.T) {
	var inner, relay []string
	tee := teeStreamSink{inner: markingSink{&inner}, relay: plainSink{&relay}}
	tee.OnDelta("hi")
	tee.OnStreamEnd()
	if want := []string{"hi", "<end>"}; !reflect.DeepEqual(inner, want) {
		t.Fatalf("inner = %q, want %q", inner, want)
	}
	if want := []string{"hi"}; !reflect.DeepEqual(relay, want) {
		t.Fatalf("relay = %q, want %q", relay, want)
	}
}
