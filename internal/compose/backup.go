package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// maxConfigOutput bounds the rendered Compose configuration; more than this is a failure.
const maxConfigOutput = 4 << 20

// DefaultConfigTimeout limits the call that renders the configuration.
const DefaultConfigTimeout = 30 * time.Second

// Running reports whether at least one container of the stack is running.
func (x *Executor) Running(ctx context.Context, st stacks.Stack, log Logger) (bool, error) {
	cs, err := x.Ps(ctx, st, log)
	if err != nil {
		return false, err
	}
	for _, c := range cs {
		if c.State == "running" || c.State == "restarting" {
			return true, nil
		}
	}
	return false, nil
}

// VolumeNames maps each volume key of the merged Compose configuration to the name of the Docker volume:
// the project-scoped name Compose derives, or the name the file sets for an external volume. Compose resolves
// it, so no naming convention is assumed here.
//
// The rendered configuration contains interpolated values of the env files. It is read into memory,
// reduced to the volume names and never logged.
func (x *Executor) VolumeNames(ctx context.Context, st stacks.Stack, log Logger) (map[string]string, error) {
	raw, err := x.renderConfig(ctx, st, log)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Volumes map[string]struct {
			Name string `json:"name"`
		} `json:"volumes"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, errors.New("docker compose config: unexpected output")
	}
	out := make(map[string]string, len(cfg.Volumes))
	for key, v := range cfg.Volumes {
		if v.Name == "" {
			return nil, fmt.Errorf("docker compose config: volume %q has no resolved name", key)
		}
		out[key] = v.Name
	}
	return out, nil
}

// renderConfig returns the merged Compose configuration as JSON. It contains interpolated secrets: callers
// reduce it to what they need and never log it.
func (x *Executor) renderConfig(ctx context.Context, st stacks.Stack, log Logger) ([]byte, error) {
	c, err := x.command(st, st.Name, 0, "config", "--format", "json")
	if err != nil {
		return nil, err
	}
	buf := &limitBuffer{limit: maxConfigOutput}
	c.Stdout = buf
	if _, err := x.runNamed(ctx, log, c, orDefault(x.PSTimeout, DefaultConfigTimeout), "docker compose config"); err != nil {
		return nil, err
	}
	if buf.over {
		return nil, errors.New("docker compose config: the configuration is too large")
	}
	return bytes.TrimSpace(buf.b.Bytes()), nil
}

// limitBuffer collects at most limit bytes and notes when more came.
type limitBuffer struct {
	b     bytes.Buffer
	limit int
	over  bool
}

func (l *limitBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := l.limit - l.b.Len(); n > room {
		l.over = true
		p = p[:max(room, 0)]
	}
	l.b.Write(p)
	return n, nil
}

var _ io.Writer = (*limitBuffer)(nil)
