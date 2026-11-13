package compose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// Logger is the part of the run log the executor writes to; *engine.Log implements it.
type Logger interface {
	Step(name string)
	Printf(format string, args ...any)
}

const (
	dockerProgram = "docker"
	envFile       = ".env"

	// fixedPath is the only search path a docker call gets.
	fixedPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

	// maxPSOutput bounds the container listing; more than this is a failure, not a truncated answer.
	maxPSOutput = 1 << 20
)

// Default time limits of one Compose call.
const (
	DefaultPullTimeout = 10 * time.Minute
	DefaultUpTimeout   = 5 * time.Minute
	DefaultStopTimeout = 2 * time.Minute
	// DefaultStartTimeout and DefaultRestartTimeout bound a start and a restart in place.
	DefaultStartTimeout   = 2 * time.Minute
	DefaultRestartTimeout = 3 * time.Minute
	DefaultPSTimeout      = 30 * time.Second
)

// projectName is what Compose accepts as a project name.
var projectName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ProductionEnv is the environment of every docker call in the service: a fixed PATH and C locale, plus HOME
// and DOCKER_CONFIG when the service has them, because docker reads its registry credentials from there.
// DOCKER_HOST, COMPOSE_* and proxy variables of the service are not passed on.
func ProductionEnv(getenv func(string) string) []string {
	env := []string{"PATH=" + fixedPath, "LC_ALL=C"}
	for _, k := range []string{"HOME", "DOCKER_CONFIG"} {
		if v := getenv(k); v != "" && !strings.ContainsAny(v, "\x00\n") {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// Executor runs Docker Compose for one stack at a time. Runner is required.
type Executor struct {
	Runner Runner
	// Env is the complete environment of every call.
	Env []string
	// Time limits per call; zero means the Default constant.
	PullTimeout, UpTimeout, StopTimeout, StartTimeout, RestartTimeout, PSTimeout time.Duration
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// Pull pulls the images of the stack.
func (x *Executor) Pull(ctx context.Context, st stacks.Stack, log Logger) error {
	return x.step(ctx, st, log, orDefault(x.PullTimeout, DefaultPullTimeout), "pull", "pull")
}

// Up starts the stack detached and removes containers of services that are gone.
func (x *Executor) Up(ctx context.Context, st stacks.Stack, log Logger) error {
	return x.step(ctx, st, log, orDefault(x.UpTimeout, DefaultUpTimeout), "up", "up", "-d", "--remove-orphans")
}

// Stop stops the containers of the stack without removing them.
func (x *Executor) Stop(ctx context.Context, st stacks.Stack, log Logger) error {
	return x.step(ctx, st, log, orDefault(x.StopTimeout, DefaultStopTimeout), "stop", "stop")
}

// Start starts the existing, stopped containers of the stack; it creates none.
func (x *Executor) Start(ctx context.Context, st stacks.Stack, log Logger) error {
	return x.step(ctx, st, log, orDefault(x.StartTimeout, DefaultStartTimeout), "start", "start")
}

// Restart restarts the containers of the stack without recreating them.
func (x *Executor) Restart(ctx context.Context, st stacks.Stack, log Logger) error {
	return x.step(ctx, st, log, orDefault(x.RestartTimeout, DefaultRestartTimeout), "restart", "restart")
}

func (x *Executor) step(ctx context.Context, st stacks.Stack, log Logger, timeout time.Duration, label string, words ...string) error {
	c, err := x.command(st, st.Name, 0, words...)
	if err != nil {
		return err
	}
	res, err := x.run(ctx, log, c, timeout, label)
	logOutput(log, res)
	return err
}

// run starts c under its own time limit and logs the command line.
func (x *Executor) run(ctx context.Context, log Logger, c Command, timeout time.Duration, label string) (Result, error) {
	return x.runNamed(ctx, log, c, timeout, "docker compose "+label)
}

// Do runs c, a call the caller assembled itself, under the executor's runner with its own time limit. name
// says what the call is in the error text. The command line is logged, its output is not.
func (x *Executor) Do(ctx context.Context, log Logger, c Command, timeout time.Duration, name string) (Result, error) {
	return x.runNamed(ctx, log, c, timeout, name)
}

func (x *Executor) runNamed(ctx context.Context, log Logger, c Command, timeout time.Duration, name string) (Result, error) {
	if x.Runner == nil {
		return Result{}, errors.New("executor has no runner")
	}
	log.Printf("run: %s %s", c.Name, strings.Join(c.Args, " "))
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := x.Runner.Run(cctx, c)
	if err != nil && cctx.Err() != nil {
		if ctx.Err() == nil {
			return res, fmt.Errorf("%s timed out after %s", name, timeout)
		}
		return res, fmt.Errorf("canceled: %w", ctx.Err())
	}
	if err != nil {
		return res, fmt.Errorf("%s failed: %w", name, err)
	}
	return res, nil
}

func logOutput(log Logger, res Result) {
	if text := strings.TrimRight(string(res.Output), "\r\n"); text != "" {
		log.Printf("output:\n%s", text)
	}
}

// command builds one docker compose call:
//
//	docker compose [--project-name <stack>] --project-directory <dir> [--env-file .env] [--env-file secrets.env]
//	  -f <file> [-f <file> ...] <words>
//
// Every compose file is passed with -f in merge order, so COMPOSE_FILE from .env is never consulted and the
// files are exactly those qdeploy read and validated. An env file is passed only when it exists. The project
// name is left to Compose when qdeploy resolved it the way Compose does (Stack.NameDerived) and secrets.env
// cannot change it; otherwise it is forced. It fails, before anything runs, when an env file is unusable or a
// name is unusable.
func (x *Executor) command(st stacks.Stack, project string, maxOutput int, words ...string) (Command, error) {
	if st.Err != nil {
		return Command{}, fmt.Errorf("stack %s cannot be deployed: %w", st.Name, st.Err)
	}
	if !projectName.MatchString(project) {
		return Command{}, fmt.Errorf("stack name %q is not a valid Compose project name (lowercase letters, digits, - and _)", project)
	}
	dir, envFiles, secretsSetName, err := inputsOf(st)
	if err != nil {
		return Command{}, err
	}
	args := []string{"compose"}
	if !st.NameDerived || project != st.Name || secretsSetName {
		args = append(args, "--project-name", project)
	}
	args = append(args, "--project-directory", dir)
	for _, f := range envFiles {
		args = append(args, "--env-file", f)
	}
	for _, f := range st.ComposeFiles {
		args = append(args, "-f", f)
	}
	args = append(args, words...)
	return Command{Name: dockerProgram, Args: args, Dir: dir, Env: x.Env, MaxOutput: maxOutput}, nil
}

// inputsOf validates the files of st and returns its absolute folder and the env files qdeploy passes, relative
// to it. It is the one place that decides which files a call gets.
func inputsOf(st stacks.Stack) (dir string, envFiles []string, secretsSetName bool, err error) {
	if st.Dir == "" {
		return "", nil, false, errors.New("stack has no directory")
	}
	dir, err = filepath.Abs(st.Dir)
	if err != nil {
		return "", nil, false, fmt.Errorf("stack directory: %w", err)
	}
	if len(st.ComposeFiles) == 0 {
		return "", nil, false, errors.New("stack has no compose file")
	}
	envFiles, secretsSetName, err = envFilesOf(dir)
	if err != nil {
		return "", nil, false, err
	}
	for _, f := range st.ComposeFiles {
		if err := composeFileName(f); err != nil {
			return "", nil, false, fmt.Errorf("compose file %q: %w", f, err)
		}
	}
	return dir, envFiles, secretsSetName, nil
}

// DeployInputs is what qdeploy hands to docker compose for a stack, with absolute paths: the project name
// the containers get and the files in the order of the call.
type DeployInputs struct {
	Project      string
	ComposeFiles []string
	EnvFiles     []string
}

// InputsOf returns the project name and the compose and env files a deploy of st passes to docker compose. It
// fails for a stack that cannot be deployed, with the error of the deploy.
func InputsOf(st stacks.Stack) (DeployInputs, error) {
	if st.Err != nil {
		return DeployInputs{}, fmt.Errorf("stack %s cannot be deployed: %w", st.Name, st.Err)
	}
	if !projectName.MatchString(st.Name) {
		return DeployInputs{}, fmt.Errorf("stack name %q is not a valid Compose project name", st.Name)
	}
	dir, envFiles, _, err := inputsOf(st)
	if err != nil {
		return DeployInputs{}, err
	}
	in := DeployInputs{Project: st.Name}
	for _, f := range envFiles {
		in.EnvFiles = append(in.EnvFiles, filepath.Join(dir, f))
	}
	for _, f := range st.ComposeFiles {
		in.ComposeFiles = append(in.ComposeFiles, filepath.Join(dir, f))
	}
	return in, nil
}

// composeFileName accepts only a clean relative path below the stack folder.
func composeFileName(f string) error {
	switch {
	case f == "" || f == ".":
		return errors.New("empty path")
	case filepath.IsAbs(f) || strings.HasPrefix(f, "-") || strings.ContainsAny(f, "\x00\\\n"):
		return errors.New("not a relative path inside the stack folder")
	case filepath.Clean(f) != f || f == ".." || strings.HasPrefix(f, "../"):
		return errors.New("path must be clean and stay inside the stack folder")
	}
	return nil
}

// envFilesOf returns the env files that exist, .env first, without reading a value. secretsSetName says
// secrets.env sets COMPOSE_PROJECT_NAME, which Compose then prefers over .env. Both files are optional;
// one that exists must be usable.
func envFilesOf(dir string) (files []string, secretsSetName bool, err error) {
	info, err := os.Lstat(filepath.Join(dir, envFile))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, false, fmt.Errorf("%s: %w", envFile, err)
	case !info.Mode().IsRegular():
		return nil, false, fmt.Errorf("%s is not a regular file", envFile)
	default:
		files = append(files, envFile)
	}
	keys, exists, err := stacks.SecretKeys(dir)
	if err != nil {
		return nil, false, fmt.Errorf("%s is not usable: %w", stacks.SecretsFile, err)
	}
	if exists {
		files = append(files, stacks.SecretsFile)
	}
	return files, slices.Contains(keys, "COMPOSE_PROJECT_NAME"), nil
}
