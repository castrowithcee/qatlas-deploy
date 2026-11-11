package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

func newSourceListCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the bound stack folders with kind, address, head and state of the last poll",
		Long: "list shows every bound stack folder in the order it was bound: its path, id, kind (remote or local), the\n" +
			"repository address (none for a local folder), the head commit, the time of the last poll, a poll error\n" +
			"and a note about why the working copy was not brought up to date. It changes nothing. Run it with sudo.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var res control.SourceListResult
			if err := credentialCall(c, state, control.SourceListCommand, nil, &res); err != nil {
				return err
			}
			return output.Encode(c.OutOrStdout(), program.Format(), sourceListResult(program.Format(), res))
		},
	}
}

func sourceListResult(format output.Format, res control.SourceListResult) output.Object {
	if structuredFormat(format) {
		sources := res.Sources
		if sources == nil {
			sources = []control.SourceStatus{}
		}
		return output.Object{Fields: []output.Field{{Name: "sources", Value: structured(format, sources)}}}
	}
	var f []output.Field
	for _, src := range res.Sources {
		p := "source." + src.ID + "."
		f = append(f, output.Field{Name: p + "path", Value: src.Path}, output.Field{Name: p + "kind", Value: src.Kind})
		if !src.LastPoll.IsZero() {
			f = append(f, output.Field{Name: p + "last_poll", Value: src.LastPoll.UTC().Format(time.RFC3339)})
		}
		for _, kv := range [][2]string{{"url", src.URL}, {"head", src.Head}, {"poll_error", src.PollError}, {"note", src.Note}} {
			if kv[1] != "" {
				f = append(f, output.Field{Name: p + kv[0], Value: kv[1]})
			}
		}
	}
	return output.Object{Fields: f}
}

func newSourceRemoveCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	var gate *cobracli.Gate
	remove := &cobra.Command{
		Use:   "remove <path>",
		Short: "Release a bound stack folder; no file is deleted and no container is stopped",
		Long: "remove releases the binding of the folder and drops the engine state of the source. No file in the folder\n" +
			"is deleted or changed, no container is stopped, and the permissions, the systemd drop-in entry and the\n" +
			"deploy key stay, so the folder can be bound again with 'source add'. It asks first: at a terminal by\n" +
			"answering the question, without a terminal with --yes. A source with an active run is refused. Run it\n" +
			"with sudo.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageFailure("source remove takes exactly one argument, the path of the folder",
					"Run 'qdeploy source remove <path>'.")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			path := args[0]
			if !filepath.IsAbs(path) || filepath.Clean(path) != path {
				return usageFailure("the path must be absolute and clean", "Pass the absolute path of the folder.")
			}
			w := c.ErrOrStderr()
			_, _ = fmt.Fprintf(w, "Release the stack folder %s:\n", path)
			for _, l := range []string{
				"Only the binding is released and the engine state of the source is removed.",
				"No file in the folder is deleted, no container is stopped.",
				"The permissions, the systemd drop-in entry and the deploy key stay, so the folder can be bound again.",
			} {
				_, _ = fmt.Fprintln(w, "  "+l)
			}
			question := fmt.Sprintf("Release the stack folder %s?", path)
			return gate.Run(cobracli.Request{Question: question}, func() error {
				var res control.SourceRemoveResult
				if err := credentialCall(c, state, control.SourceRemoveCommand, control.SourceRemoveArgs{Path: path}, &res); err != nil {
					return sourceRemoveFailure(err)
				}
				return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
					{Name: "id", Value: res.ID}, {Name: "path", Value: res.Path},
				}})
			})
		},
	}
	gate = program.AddGate(remove, cobracli.Gates{Question: true})
	return remove
}

// sourceRemoveFailure words an unknown folder for this command; failureOf's hint for it belongs to keys.
func sourceRemoveFailure(err error) error {
	var remote *control.Error
	if errors.As(err, &remote) && remote.Code == control.CodeNotFound {
		return &cli.Failure{Code: output.CodeNotFound, Message: remote.Message,
			NextStep: "List the bound folders with 'qdeploy source list'.", Cause: err}
	}
	return err
}
