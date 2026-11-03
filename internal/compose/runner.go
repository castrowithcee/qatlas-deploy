// Package compose deploys a stack with Docker Compose and checks that it runs healthy. Every program is
// started with argv and an explicit environment, never through a shell; stack names, compose file names and
// URLs from the working copy are data and are validated before they reach a command line.
package compose

import (
	"context"
	"fmt"
)

// Command is one external program call. Name and Args are passed as argv.
type Command struct {
	Name string
	Args []string
	// Dir is the working directory.
	Dir string
	// Env is the complete environment of the program; nothing is inherited.
	Env []string
	// MaxOutput caps the captured output; zero means DefaultMaxOutput.
	MaxOutput int
}

// Result is what a finished program produced.
type Result struct {
	// Output is the combined standard output and error, capped by the runner.
	Output []byte
	// Omitted counts the output bytes the runner dropped.
	Omitted int64
}

// Runner starts external programs. Run returns an *ExitError for a non-zero exit status, the context's error
// when the context ended the program, and any other error when the program could not run. The Result is
// filled whenever the program started.
type Runner interface {
	Run(ctx context.Context, c Command) (Result, error)
}

// ExitError reports a program that ended with a non-zero exit status (-1 when a signal ended it).
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }
