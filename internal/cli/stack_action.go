package cli

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

var stackActionCommands = map[string]string{
	"start": control.StackStartCommand, "stop": control.StackStopCommand, "restart": control.StackRestartCommand,
}

func newStackActionCommand(program *cobracli.Program, state *notRunning, verb string) *cobra.Command {
	var gate *cobracli.Gate
	long := map[string]string{
		"start": "start starts the existing, stopped containers of the stack, clears the stopped mark and runs the\n" +
			"stack's check. It never creates containers: a stack that was never deployed or whose containers are\n" +
			"removed fails; deploy it with 'qdeploy stack deploy'.",
		"stop": "stop stops the containers of the stack without removing them and sets the stopped mark: new commits\n" +
			"of the stacks repository, the maintenance window and updates leave the stack stopped, and 'qdeploy status'\n" +
			"and 'qdeploy stack list' show it. Only 'qdeploy stack start' clears the mark.",
		"restart": "restart restarts the containers of the stack and runs the stack's check. A stack stopped through\n" +
			"qdeploy is refused: use start.",
	}[verb]
	cmd := &cobra.Command{
		Use:   verb + " <stack> [--yes]",
		Short: map[string]string{"start": "Start a stopped stack", "stop": "Stop a stack and keep it stopped", "restart": "Restart a stack"}[verb],
		Long: long + "\n\nThe command is refused while a run of the stack is active. At a terminal it asks first (default no);\n" +
			"without a terminal it refuses unless --yes is given (with --agent, --yes is always needed). Run it with\n" +
			"sudo. See 'qdeploy help containers'.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			question := strings.ToUpper(verb[:1]) + verb[1:] + " stack " + args[0] + "?"
			if verb == "stop" {
				question = "Stop stack " + args[0] + " and keep it stopped until it is started?"
			}
			return gate.Run(cobracli.Request{Question: question}, func() error {
				var res control.StackActionResult
				err := callFor(c.Context(), control.StackActionTimeout, serviceEndpoint(), stackActionCommands[verb],
					control.StackActionArgs{Stack: args[0]}, &res)
				if errors.Is(err, client.ErrNotRunning) {
					state.seen = true
				}
				if err != nil {
					return failureOf(err)
				}
				return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
					{Name: "stack", Value: res.Stack}, {Name: "action", Value: res.Action}, {Name: "stopped", Value: res.Stopped},
				}})
			})
		},
	}
	gate = program.AddGate(cmd, cobracli.Gates{Question: true})
	return cmd
}
