package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// Defaults of the health check.
const (
	DefaultContainerDeadline = 2 * time.Minute
	DefaultPollInterval      = 2 * time.Second

	// maxProbeBody bounds what is read of an HTTP check response; the body is never judged.
	maxProbeBody = 64 << 10
	// maxRequestTimeout bounds one HTTP request inside the overall timeout of the check.
	maxRequestTimeout = 10 * time.Second
)

// Container is the state of one container of the project as `docker compose ps` reports it.
type Container struct {
	Name     string `json:"Name"`
	Service  string `json:"Service"`
	State    string `json:"State"`
	Health   string `json:"Health"`
	ExitCode int    `json:"ExitCode"`
}

func (c Container) String() string {
	name := c.Service
	if name == "" {
		name = c.Name
	}
	if c.Health != "" {
		return name + "=" + c.State + "/" + c.Health
	}
	return name + "=" + c.State
}

// Ps lists every container of the project, stopped ones included.
func (x *Executor) Ps(ctx context.Context, st stacks.Stack, log Logger) ([]Container, error) {
	c, err := x.command(st, st.Name, maxPSOutput, "ps", "--all", "--format", "json")
	if err != nil {
		return nil, err
	}
	res, err := x.run(ctx, log, c, orDefault(x.PSTimeout, DefaultPSTimeout), "ps")
	if err != nil {
		return nil, err
	}
	if res.Omitted > 0 {
		return nil, errors.New("docker compose ps: the container listing is too large")
	}
	return parsePS(res.Output)
}

// parsePS reads both shapes Compose emits: one JSON object per line, or one array.
func parsePS(out []byte) ([]Container, error) {
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil, nil
	}
	var cs []Container
	if out[0] == '[' {
		if err := json.Unmarshal(out, &cs); err != nil {
			return nil, errors.New("docker compose ps: unexpected output")
		}
		return cs, nil
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var c Container
		if err := dec.Decode(&c); errors.Is(err, io.EOF) {
			return cs, nil
		} else if err != nil {
			return nil, errors.New("docker compose ps: unexpected output")
		}
		cs = append(cs, c)
	}
}

// judge decides whether the containers are up. ready means all run and every container with a healthcheck is
// healthy; fatal is a state that cannot become ready by waiting.
func judge(cs []Container) (ready bool, fatal error, summary string) {
	if len(cs) == 0 {
		return false, nil, "no containers"
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].String() < cs[j].String() })
	parts := make([]string, len(cs))
	ready = true
	for i, c := range cs {
		parts[i] = c.String()
		switch {
		case c.State == "exited" || c.State == "dead":
			fatal = errors.Join(fatal, fmt.Errorf("container %s exited with code %d", c.String(), c.ExitCode))
		case c.Health == "unhealthy":
			fatal = errors.Join(fatal, fmt.Errorf("container %s is unhealthy", c.String()))
		}
		if c.State != "running" || (c.Health != "" && c.Health != "healthy") {
			ready = false
		}
	}
	return ready, fatal, strings.Join(parts, " ")
}

// Checker decides when a deployed stack counts as healthy. Exec is required; the rest has defaults.
type Checker struct {
	Exec *Executor
	// Deadline is how long containers may take to become ready; zero means DefaultContainerDeadline.
	Deadline time.Duration
	// Interval is the pause between two polls; zero means DefaultPollInterval.
	Interval time.Duration
	// Now and Sleep are the clock; tests replace them. Sleep returns the context's error when it ends first.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
	// HTTPClient performs the HTTP check. Redirects are never followed and the proxy variables are ignored
	// unless a client is set here.
	HTTPClient *http.Client
}

func (k *Checker) now() time.Time {
	if k.Now != nil {
		return k.Now()
	}
	return time.Now()
}

func (k *Checker) sleep(ctx context.Context, d time.Duration) error {
	if k.Sleep != nil {
		return k.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (k *Checker) interval() time.Duration { return orDefault(k.Interval, DefaultPollInterval) }

// Check applies the check rules of the stack: containers (default on) and the optional HTTP probe.
func (k *Checker) Check(ctx context.Context, st stacks.Stack, log Logger) error {
	rules := stacks.Check{Containers: true}
	if st.Rules != nil {
		rules = st.Rules.Check
	}
	if rules.Containers {
		if err := k.checkContainers(ctx, st, log); err != nil {
			return err
		}
	}
	if rules.HTTP != nil {
		return k.checkHTTP(ctx, *rules.HTTP, log)
	}
	return nil
}

func (k *Checker) checkContainers(ctx context.Context, st stacks.Stack, log Logger) error {
	deadline := k.now().Add(orDefault(k.Deadline, DefaultContainerDeadline))
	for {
		cs, err := k.Exec.Ps(ctx, st, log)
		if err != nil {
			return err
		}
		ready, fatal, summary := judge(cs)
		log.Printf("containers: %s", summary)
		if fatal != nil {
			return fatal
		}
		if ready {
			return nil
		}
		if !k.now().Before(deadline) {
			return fmt.Errorf("containers not ready in time: %s", summary)
		}
		if err := k.sleep(ctx, k.interval()); err != nil {
			return fmt.Errorf("canceled while waiting for containers (%s): %w", summary, err)
		}
	}
}

func (k *Checker) client() *http.Client {
	var c http.Client
	if k.HTTPClient != nil {
		c = *k.HTTPClient
	} else {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.Proxy = nil
		c.Transport = t
	}
	// The first status is judged as the server sends it; a redirect is never followed.
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// checkHTTP polls the URL of the stack settings until it answers with the wanted status; the configured timeout is
// the whole time allowed for the check.
func (k *Checker) checkHTTP(ctx context.Context, h stacks.HTTPCheck, log Logger) error {
	u, err := url.Parse(h.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("http check: the URL is not an absolute http or https URL")
	}
	client := k.client()
	deadline := k.now().Add(h.Timeout)
	for {
		perTry := min(h.Timeout, maxRequestTimeout)
		last := probe(ctx, client, h.URL, h.Status, perTry)
		if last == nil {
			log.Printf("http check: %s answered %d", h.URL, h.Status)
			return nil
		}
		log.Printf("http check: %v", last)
		if !k.now().Before(deadline) {
			return fmt.Errorf("http check failed: %w", last)
		}
		if err := k.sleep(ctx, k.interval()); err != nil {
			return fmt.Errorf("canceled during the http check (%v): %w", last, err)
		}
	}
}

// probe performs one GET. The error text names the status or the cause, never the URL again.
func probe(ctx context.Context, client *http.Client, target string, want int, timeout time.Duration) error {
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, target, nil)
	if err != nil {
		return errors.New("invalid request")
	}
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxProbeBody))
	if resp.StatusCode != want {
		return fmt.Errorf("status %d, want %d", resp.StatusCode, want)
	}
	return nil
}
