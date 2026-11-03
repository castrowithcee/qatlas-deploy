package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// backupCallLimit bounds create and restore, which wait for the stack to stop, archive and start again.
const backupCallLimit = 6*time.Hour + time.Minute

func newBackupCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	group := &cobra.Command{
		Use:   "backup",
		Short: "Back up, list and restore the data of a stack",
		Long: "A backup saves the data directories and named volumes that the stack's qdeploy.yaml lists, and the\n" +
			"output of its backup command. The stack is stopped while it is saved and started again afterwards.\n" +
			"Backups are kept under the service's state directory, readable by the service user only. Only root\n" +
			"may use the service channel, so run these commands with sudo.",
	}
	var gate *cobracli.Gate
	create := &cobra.Command{
		Use:   "create <stack>",
		Short: "Stop a stack, back up its data and start it again",
		Long: "create stops the stack if it runs, saves each listed data directory (below data/) and volume with a\n" +
			"helper container without network, runs the backup command if the rules define one, starts the stack\n" +
			"again and then removes the backups beyond the retention limits. It refuses a stack with a run in\n" +
			"progress or local changes. If anything fails the partial backup is removed and the stack is started\n" +
			"again. The command waits until the backup is finished.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.BackupCreateResult
			if err := backupCall(c, state, control.BackupCreateCommand, control.BackupArgs{Stack: args[0]}, &res); err != nil {
				return err
			}
			b := res.Backup
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
				{Name: "backup", Value: b.ID}, {Name: "stack", Value: b.Stack},
				{Name: "created", Value: b.Created.UTC().Format(time.RFC3339)},
				{Name: "commit", Value: b.Commit}, {Name: "size", Value: b.Size},
				{Name: "items", Value: int64(len(b.Items))},
				{Name: "removed", Value: strings.Join(res.Removed, ",")},
			}})
		},
	}
	list := &cobra.Command{
		Use:   "list <stack>",
		Short: "List the backups of a stack, newest first",
		Long: "list shows the backups of a stack with creation time, deployed commit, size and number of parts. A\n" +
			"directory of the store without valid metadata shows as incomplete.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.BackupListResult
			if err := backupCall(c, state, control.BackupListCommand, control.BackupArgs{Stack: args[0]}, &res); err != nil {
				return err
			}
			rows := make([]output.Row, 0, len(res.Backups))
			for _, b := range res.Backups {
				commit := b.Commit
				if len(commit) > 12 && program.Format() == output.FormatTable {
					commit = commit[:12]
				}
				status, created := "complete", b.Created.UTC().Format(time.RFC3339)
				if b.Incomplete {
					status, created = "incomplete", ""
				}
				rows = append(rows, output.Row{"backup": b.ID, "created": created, "commit": commit,
					"size": b.Size, "items": int64(len(b.Items)), "status": status})
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Collection{
				Columns: []string{"backup", "created", "commit", "size", "items", "status"}, Rows: rows})
		},
	}
	restore := &cobra.Command{
		Use:   "restore <stack> <backup> [--yes]",
		Short: "Replace the data of a stack with a backup",
		Long: "restore stops the stack if it runs, empties each data directory and volume the backup holds and\n" +
			"unpacks the backup into it with owners and permissions, then starts the stack again. It needs the\n" +
			"id of one backup from 'qdeploy stack backup list'; the newest is never chosen for you. The current data\n" +
			"is overwritten. At a terminal the command asks first (default no); without a terminal it refuses\n" +
			"unless --yes is given (with --agent, --yes is always needed). A backup that holds a part the stack's rules no longer list is refused. The\n" +
			"output of the backup command is not restored. If the restore fails after the first target was\n" +
			"emptied the stack stays stopped.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return usageFailure("backup restore takes exactly two arguments, the stack and the backup id",
					"Run 'qdeploy stack backup list <stack>', then 'qdeploy stack backup restore <stack> <backup>'.")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			return runBackupRestore(c, program, gate, state, args[0], args[1])
		},
	}
	gate = program.AddGate(restore, cobracli.Gates{Question: true})
	group.AddCommand(create, list, restore)
	return group
}

func runBackupRestore(c *cobra.Command, program *cobracli.Program, gate *cobracli.Gate, state *notRunning, stack, id string) error {
	if !runIDShape.MatchString(id) {
		return &cli.Failure{Code: output.CodeNotFound, Message: "no such backup",
			NextStep: "List the backups with 'qdeploy stack backup list " + stack + "'.", Cause: fmt.Errorf("malformed backup id")}
	}
	question := fmt.Sprintf("Restore backup %s of stack %s? The stack is stopped and its current data is overwritten.", id, stack)
	return gate.Run(cobracli.Request{Question: question}, func() error {
		var res control.BackupRestoreResult
		args := control.BackupRestoreArgs{Stack: stack, ID: id, Confirm: true}
		if err := backupCall(c, state, control.BackupRestoreCommand, args, &res); err != nil {
			return err
		}
		return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
			{Name: "backup", Value: res.Backup.ID}, {Name: "stack", Value: res.Backup.Stack}, {Name: "restored", Value: true},
		}})
	})
}

func backupCall(c *cobra.Command, state *notRunning, command string, args, result any) error {
	return remoteCall(c, state, backupCallLimit, command, args, result)
}
