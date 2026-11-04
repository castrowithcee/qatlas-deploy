package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/datadir"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
)

// datadirAPI is what the command needs from the running service; tests replace it.
type datadirAPI interface {
	// Missing returns the data directories of the stack's data-dir-missing finding; found is false when the
	// stack is unknown to the service.
	Missing(ctx context.Context, stack string) (dirs []control.DirInfo, found bool, err error)
	// StackDir returns the absolute folder of the stack.
	StackDir(ctx context.Context, stack string) (string, error)
	Deploy(ctx context.Context, stack string) (control.DeployResult, error)
}

var (
	datadirEuid   = os.Geteuid
	newDatadirAPI = func(c *cobra.Command, state *notRunning) datadirAPI { return serviceDatadir{c: c, state: state} }
)

type serviceDatadir struct {
	c     *cobra.Command
	state *notRunning
}

func (s serviceDatadir) Missing(_ context.Context, stack string) ([]control.DirInfo, bool, error) {
	var st control.StatusResult
	if err := credentialCall(s.c, s.state, control.StatusCommand, nil, &st); err != nil {
		return nil, false, err
	}
	for _, sk := range st.Engine.Stacks {
		if sk.Name != stack {
			continue
		}
		for _, f := range sk.Findings {
			if f.Kind == engine.FindingDataDirMissing {
				return f.Dirs, true, nil
			}
		}
		return nil, true, nil
	}
	return nil, false, nil
}

func (s serviceDatadir) StackDir(_ context.Context, stack string) (string, error) {
	var res control.StacksResult
	if err := credentialCall(s.c, s.state, control.StacksListCommand, nil, &res); err != nil {
		return "", err
	}
	return filepath.Join(res.Dir, stack), nil
}

func (s serviceDatadir) Deploy(_ context.Context, stack string) (control.DeployResult, error) {
	var res control.DeployResult
	err := credentialCall(s.c, s.state, control.DeployCommand, control.DeployArgs{Stack: stack}, &res)
	return res, err
}

func newDatadirCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	var owner, mode string
	var gate *cobracli.Gate
	cmd := &cobra.Command{
		Use:   "datadir <stack> [--owner UID:GID] [--mode 0750] [--yes]",
		Short: "Create the missing data directories of a stack and deploy it",
		Long: "datadir creates the data directories the stack's finding 'data-dir-missing' lists, below the stack folder,\n" +
			"with the given owner and mode, then starts a run of the stack like 'qdeploy stack deploy'. Without a\n" +
			"finding it changes nothing. The owner defaults to the one the finding proposes; if it proposes none or\n" +
			"different ones, give --owner. The mode defaults to 0750 and may hold permission bits only. Existing\n" +
			"directories are never changed, missing parents are created like the listed directories, and nothing\n" +
			"outside the stack folder is touched; a symlink on the way refuses the command.\n\n" +
			"The command prints what it would create and, at a terminal, asks first (default no); without a terminal it\n" +
			"creates nothing unless --yes is given (with --agent, --yes is always needed). Needs root: run it with\n" +
			"sudo. See 'qdeploy help data-directories'.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return runDatadir(c, program, gate, newDatadirAPI(c, state), args[0], owner, mode)
		},
	}
	cmd.Flags().StringVar(&owner, "owner", "", "owner of the created directories as UID:GID (default: the proposal of the finding)")
	cmd.Flags().StringVar(&mode, "mode", "0750", "octal permission mode of the created directories")
	gate = program.AddGate(cmd, cobracli.Gates{Question: true})
	return cmd
}

func runDatadir(c *cobra.Command, program *cobracli.Program, gate *cobracli.Gate, api datadirAPI, stack, ownerFlag, modeFlag string) error {
	ctx := c.Context()
	if datadirEuid() != 0 {
		return &cli.Failure{Code: output.CodePermission, Message: "creating data directories needs root; run with sudo",
			NextStep: "Run the same command with sudo."}
	}
	mode, err := datadir.ParseMode(modeFlag)
	if err != nil {
		return usageFailure(err.Error(), "Give --mode as an octal permission mode such as 0750.")
	}
	var chosen *datadir.Owner
	if ownerFlag != "" {
		o, err := datadir.ParseOwner(ownerFlag)
		if err != nil {
			return usageFailure(err.Error(), "Give --owner as UID:GID, for example 1000:1000.")
		}
		chosen = &o
	}
	dirs, found, err := api.Missing(ctx, stack)
	if err != nil {
		return err
	}
	if !found {
		return &cli.Failure{Code: output.CodeNotFound, Message: "no such stack: " + stack,
			NextStep: "List the stacks with 'qdeploy stack list'."}
	}
	if len(dirs) == 0 {
		return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
			{Name: "stack", Value: stack}, {Name: "result", Value: "no data directory is missing; nothing changed"},
		}})
	}
	paths := make([]string, len(dirs))
	proposals := map[string]bool{}
	for i, d := range dirs {
		paths[i] = d.Path
		proposals[d.Owner] = true
	}
	if chosen == nil {
		if len(proposals) != 1 || proposals[""] {
			return usageFailure("the finding proposes no single owner for "+strings.Join(paths, ", "),
				"Run the command again with --owner UID:GID, the user the container runs as.")
		}
		o, err := datadir.ParseOwner(dirs[0].Owner)
		if err != nil {
			return usageFailure("the proposed owner is not valid: "+err.Error(), "Run the command again with --owner UID:GID.")
		}
		chosen = &o
	}
	dir, err := api.StackDir(ctx, stack)
	if err != nil {
		return err
	}
	verb := "Would create"
	if yes, _ := c.Flags().GetBool("yes"); yes {
		verb = "Creating"
	}
	if _, err := fmt.Fprintf(c.ErrOrStderr(), "%s in %s, owner %s, mode %04o:\n  %s\n",
		verb, dir, chosen, mode, strings.Join(paths, "\n  ")); err != nil {
		return err
	}
	question := fmt.Sprintf("Create %d data director%s of stack %s as %s with mode %04o and deploy it?",
		len(paths), map[bool]string{true: "y", false: "ies"}[len(paths) == 1], stack, chosen, mode)
	return gate.Run(cobracli.Request{Question: question}, func() error {
		res, err := datadir.Create(datadir.Request{StackDir: dir, Paths: paths, Owner: *chosen, Mode: mode})
		if err != nil {
			return &cli.Failure{Code: output.CodeRuntime, Message: "cannot create the data directories: " + err.Error(),
				NextStep: "Fix the cause and run the command again; directories already made stay.", Cause: err}
		}
		dep, err := api.Deploy(ctx, stack)
		if err != nil {
			_, _ = fmt.Fprintf(c.ErrOrStderr(), "Created: %s\n", strings.Join(res.Created, ", "))
			return err
		}
		return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
			{Name: "stack", Value: stack}, {Name: "owner", Value: chosen.String()}, {Name: "mode", Value: fmt.Sprintf("%04o", mode)},
			{Name: "created", Value: strings.Join(res.Created, ", ")}, {Name: "parents", Value: strings.Join(res.Parents, ", ")},
			{Name: "existing", Value: strings.Join(res.Existing, ", ")},
			{Name: "run", Value: dep.ID}, {Name: "commit", Value: dep.Commit},
		}})
	})
}
