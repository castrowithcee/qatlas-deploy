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

func TestPrimaryEntryOfAName(t *testing.T) {
	collision := []FindingInfo{{Kind: FindingNameCollision}}
	list := []StackStatus{
		{Name: "demo", Source: "a", Findings: collision},
		{Name: "demo", Source: "b", Removed: true},
		{Name: "demo", Source: "c"},
		{Name: "web", Source: "a", Findings: collision},
		{Name: "web", Source: "b", Findings: collision},
	}
	want := []bool{false, false, true, true, false}
	for i, w := range want {
		if got := Primary(list, i); got != w {
			t.Errorf("Primary(%d) = %v, want %v", i, got, w)
		}
	}
	if !Primary(list[1:2], 0) {
		t.Error("a single entry does not stand for its name")
	}
}

func TestStackDirUsesTheSourceOfTheHoldingStack(t *testing.T) {
	r := StacksResult{Dir: "/srv/first", Stacks: []StackInfo{
		{Name: "demo", Folder: "demo", SourceDir: "/srv/second", Collision: "taken"},
		{Name: "demo", Folder: "demo", SourceDir: "/srv/first"},
		{Name: "web", Folder: "web", SourceDir: "/srv/second"},
	}}
	if got, ok := r.StackDir("demo"); !ok || got != "/srv/first/demo" {
		t.Fatalf("demo = %q, %v", got, ok)
	}
	if got, ok := r.StackDir("web"); !ok || got != "/srv/second/web" {
		t.Fatalf("web = %q, %v", got, ok)
	}
}

func TestStackDirOfARootStackIsTheStackFolder(t *testing.T) {
	r := StacksResult{Stacks: []StackInfo{{Name: "demo-root", Folder: ".", SourceDir: "/srv/demo-root"}}}
	if got, ok := r.StackDir("demo-root"); !ok || got != "/srv/demo-root" {
		t.Fatalf("StackDir(demo-root) = %q, %v", got, ok)
	}
}
