package stacks

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestBindMountsAreTheHostPathsInsideTheStack(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo-stack")
	write(t, root, "demo-stack/compose.yaml", `services:
  app:
    image: registry.example.test/demo/app:${DEMO_VERSION}
    volumes:
      - ./uploads:/srv/uploads
      - ./config/app.ini:/etc/app.ini:ro
      - ./cache/../media/:/srv/media
      - app-data:/var/lib/app
      - /var/log/demo:/logs
      - ../shared:/shared
      - .:/srv/all
      - ${DEMO_DIR}:/srv/var
      - ~/demo:/srv/home
      - /anonymous
      - `+dir+`/inside:/srv/inside
      - type: bind
        source: ./long
        target: /srv/long
      - type: volume
        source: named
        target: /srv/named
      - {type: bind, source: /etc/demo, target: /srv/etc}
  worker:
    image: registry.example.test/demo/worker:${DEMO_VERSION}
    volumes: ["./uploads/thumbs:/thumbs"]
`)
	write(t, root, "demo-stack/compose.override.yaml", "services:\n  app:\n    volumes:\n      - ./extra:/srv/extra\n")
	write(t, root, "demo-stack/.env", "DEMO_VERSION=1.0.0\n")

	s := load(t, root)["demo-stack"]
	if s.Err != nil {
		t.Fatal(s.Err)
	}
	want := []string{"config/app.ini", "extra", "inside", "long", "media", "uploads", "uploads/thumbs"}
	if !slices.Equal(s.BindMounts, want) {
		t.Fatalf("BindMounts = %q\nwant         %q", s.BindMounts, want)
	}
}

func TestBindMountsOfAnUnparsableFileAreNone(t *testing.T) {
	s := one(t, map[string]string{"compose.yaml": "services: [\n"})
	if len(s.BindMounts) != 0 {
		t.Fatalf("BindMounts of a broken file = %q", s.BindMounts)
	}
}
