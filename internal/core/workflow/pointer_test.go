package workflow

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestResolvePointer(t *testing.T) {
	doc := json.RawMessage(`{"text":"hello","structured":{"tasks":["a","b"]},"artifacts":[{"id":"art_1","path":"r.md"}],"a/b":"slash","m~n":"tilde"}`)
	cases := []struct {
		name    string
		pointer string
		want    string
		wantErr error
	}{
		{name: "whole value", pointer: "", want: string(doc)},
		{name: "text", pointer: "/text", want: `"hello"`},
		{name: "nested structured", pointer: "/structured/tasks/1", want: `"b"`},
		{name: "artifact reference", pointer: "/artifacts/0", want: `{"id":"art_1","path":"r.md"}`},
		{name: "escaped slash token", pointer: "/a~1b", want: `"slash"`},
		{name: "escaped tilde token", pointer: "/m~0n", want: `"tilde"`},
		{name: "missing member", pointer: "/nope", wantErr: ErrPointerMiss},
		{name: "index out of range", pointer: "/artifacts/9", wantErr: ErrPointerMiss},
		{name: "descend into scalar", pointer: "/text/0", wantErr: ErrPointerMiss},
		{name: "invalid syntax", pointer: "text", wantErr: ErrInvalidPointer},
		{name: "bad escape", pointer: "/a~2b", wantErr: ErrInvalidPointer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolvePointer(doc, tc.pointer)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestParseNodeOutputSource(t *testing.T) {
	cases := []struct {
		source string
		want   string
		ok     bool
	}{
		{source: "node.research.output", want: "research", ok: true},
		{source: "node.a.b.output", want: "a.b", ok: true}, // a node id may contain dots
		{source: "workflow.input", ok: false},
		{source: "node.research.text", ok: false},
		{source: "node..output", ok: false}, // empty node id is rejected
		{source: "node.output", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			got, ok := ParseNodeOutputSource(tc.source)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("(%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}
