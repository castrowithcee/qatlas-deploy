package control

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadFrame(t *testing.T) {
	br := bufio.NewReaderSize(strings.NewReader("one\r\ntwo\n"+strings.Repeat("x", 10000)+"\nlast"), 16)
	for _, want := range []string{"one", "two", strings.Repeat("x", 10000)} {
		got, err := ReadFrame(br, 20000)
		if err != nil || string(got) != want {
			t.Fatalf("ReadFrame = %.20q, %v; want %.20q", got, err, want)
		}
	}
	if _, err := ReadFrame(br, 20000); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("incomplete frame = %v", err)
	}
	if _, err := ReadFrame(br, 20000); !errors.Is(err, io.EOF) {
		t.Fatalf("end = %v", err)
	}
}

func TestReadFrameLimit(t *testing.T) {
	if _, err := ReadFrame(bufio.NewReader(strings.NewReader(strings.Repeat("x", 11)+"\n")), 10); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over the limit = %v", err)
	}
	if got, err := ReadFrame(bufio.NewReader(strings.NewReader(strings.Repeat("x", 10)+"\n")), 10); err != nil || len(got) != 10 {
		t.Fatalf("at the limit = %q, %v", got, err)
	}
}

func TestWriteFrameLimit(t *testing.T) {
	var b bytes.Buffer
	if err := WriteFrame(&b, Request{V: Version, Command: strings.Repeat("a", 100)}, 50); !errors.Is(err, ErrTooLarge) || b.Len() != 0 {
		t.Fatalf("WriteFrame = %v, wrote %d", err, b.Len())
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	h := func(context.Context, json.RawMessage) (any, error) { return nil, nil }
	if err := r.Register("status", h); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("stacks.list", h); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "Status", "a b", "a..b", ".a", "a."} {
		if r.Register(bad, h) == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	if r.Register("status", h) == nil || r.Register("other", nil) == nil {
		t.Error("duplicate or nil handler accepted")
	}
	if _, ok := r.Lookup("status"); !ok {
		t.Error("registered command not found")
	}
	if _, ok := r.Lookup("missing"); ok {
		t.Error("unknown command found")
	}
}

func TestStackDirUsesTheFolderOfTheListedName(t *testing.T) {
	r := StacksResult{Dir: "/srv/stacks", Stacks: []StackInfo{
		{Name: "demo-project", Folder: "demo-folder"},
		{Name: "escape", Folder: "../etc"},
		{Name: "nested", Folder: "a/b"},
		{Name: "empty"},
	}}
	if got, ok := r.StackDir("demo-project"); !ok || got != "/srv/stacks/demo-folder" {
		t.Fatalf("StackDir(demo-project) = %q, %v", got, ok)
	}
	for _, name := range []string{"demo-folder", "escape", "nested", "empty", "missing"} {
		if got, ok := r.StackDir(name); ok {
			t.Errorf("StackDir(%s) = %q, want none", name, got)
		}
	}
	if _, ok := (StacksResult{Stacks: r.Stacks}).StackDir("demo-project"); ok {
		t.Error("StackDir without Dir answered")
	}
}
