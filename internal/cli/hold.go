package cli

import (
	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

const holdNote = "It is kept in the service's own state, survives a restart and is shown by 'qdeploy status'. It has nothing\n" +
	"to do with the pause 'qdeploy stack resume' clears after a failed rollback. Repeating the command changes nothing."

func newPauseCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	return newHoldCommand(program, state, "pause", control.PauseCommand, "Keep the maintenance window from running a stack",
		"pause keeps the maintenance window from starting any run of the stack. Manual runs ('qdeploy stack deploy',\n"+
			"'qdeploy stack update') and the deployment of new commits of the stacks repository are not affected. "+holdNote)
}

func newUnpauseCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	return newHoldCommand(program, state, "unpause", control.UnpauseCommand, "Let the maintenance window run a stack again",
		"unpause lifts what 'qdeploy stack pause' set, so the maintenance window may run the stack again. "+holdNote)
}

func newPinCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	return newHoldCommand(program, state, "pin", control.PinCommand, "Hold the image versions of a stack",
		"pin holds the image versions of the stack: the maintenance window never changes them, and 'qdeploy stack update'\n"+
			"refuses the stack until it is unpinned. New commits of the stacks repository are still deployed. "+holdNote)
}

func newUnpinCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	return newHoldCommand(program, state, "unpin", control.UnpinCommand, "Release the image versions of a stack",
		"unpin lifts what 'qdeploy stack pin' set, so the maintenance window and 'qdeploy stack update' may change the image\n"+
			"versions of the stack again. "+holdNote)
}

func newHoldCommand(program *cobracli.Program, state *notRunning, name, command, short, long string) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <stack>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.HoldResult
			if err := credentialCall(c, state, command, control.HoldArgs{Stack: args[0]}, &res); err != nil {
				return err
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
				{Name: "stack", Value: res.Stack}, {Name: "active", Value: res.Active}, {Name: "changed", Value: res.Changed},
			}})
		},
	}
}
