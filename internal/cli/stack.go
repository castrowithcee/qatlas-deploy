package cli

import (
	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cobracli"
)

// newStackCommand groups every command that concerns a stack, so the top level keeps only host commands.
func newStackCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	group := &cobra.Command{
		Use:   "stack",
		Short: "List, check, deploy, update, hold and back up the stacks of the host",
		Long: "The stack commands act on the stacks of the bound stacks repository through the running service.\n" +
			"Run 'qdeploy stack <command> --help' for one command.",
	}
	group.AddCommand(newStacksCommand(program, state), newCheckCommand(program, state), newDeployCommand(program, state),
		newUpdateCommand(program, state), newUpdatesCommand(program, state), newChangelogCommand(program, state),
		newRunsCommand(program, state), newLogCommand(program, state), newResumeCommand(program, state),
		newBackupCommand(program, state), newPauseCommand(program, state), newUnpauseCommand(program, state),
		newPinCommand(program, state), newUnpinCommand(program, state))
	return group
}
