package cli

import (
	"errors"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The status shown for a stack that has no per-variable result to list.
const (
	statusNeverChecked = "never-checked"
	statusNoVariables  = "no-variables"
	statusUnreadable   = "unreadable"
)

func newUpdatesCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	var refresh, withChangelog bool
	cmd := &cobra.Command{
		Use:   "updates [stack] [--refresh] [--changelog]",
		Short: "Show the current and the available image versions of the stacks",
		Long: "updates lists, per stack and image version variable, the current version, the version the stack's rules\n" +
			"allow it to move to (available) and, if a newer major version exists, major_available, which is never\n" +
			"taken automatically. The rules are the stack's update.ceiling and update.tags; a stack without a\n" +
			"qdeploy.yaml is listed too. Statuses: current, update, not-comparable (the current tag cannot be\n" +
			"ordered, for example 'latest') and error (the registry could not be asked); the error column says why.\n\n" +
			"Without --refresh the result of the last check is shown with its time; a stack never checked says so.\n" +
			"With --refresh the service asks the registries now and stores the result. Nothing in any stack, in the\n" +
			"repository or in a deployment is changed. A private registry needs a stored login (see qdeploy help\n" +
			"credentials); without one it is asked anonymously.\n\n" +
			"With --changelog the release notes between the current and the available version are shown instead of\n" +
			"the version table (see qdeploy stack changelog).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			a := control.UpdatesArgs{Refresh: refresh, Changelog: withChangelog}
			if len(args) == 1 {
				a.Stack = args[0]
			}
			limit := client.CallTimeout
			if refresh {
				limit = control.UpdatesRefreshTimeout + client.CallTimeout
			}
			if withChangelog {
				limit += control.ChangelogTimeout
			}
			var res control.UpdatesResult
			if err := callFor(c.Context(), limit, serviceEndpoint(), control.UpdatesListCommand, a, &res); err != nil {
				if errors.Is(err, client.ErrNotRunning) {
					state.seen = true
				}
				return failureOf(err)
			}
			if withChangelog {
				return encodeChangelogs(c.OutOrStdout(), program.Format(), changelogsOfUpdates(res))
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Collection{Columns: updateColumns, Rows: updateRows(res)})
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "ask the registries now and store the result")
	cmd.Flags().BoolVar(&withChangelog, "changelog", false, "show the release notes of the available updates instead of the version table")
	return cmd
}

var updateColumns = []string{"stack", "update", "variable", "current", "available", "major_available", "status", "checked", "error"}

func updateRows(res control.UpdatesResult) []output.Row {
	var rows []output.Row
	for _, s := range res.Stacks {
		base := output.Row{"stack": s.Name, "update": s.Update}
		if !s.Checked.IsZero() {
			base["checked"] = s.Checked.UTC().Format(time.RFC3339)
		}
		only := func(status, text string) {
			row := output.Row{"stack": s.Name, "update": s.Update, "status": status, "error": text}
			rows = append(rows, row)
		}
		switch {
		case s.Error != "":
			only(statusUnreadable, s.Error)
		case s.Checked.IsZero():
			only(statusNeverChecked, "")
		case len(s.Items) == 0:
			base["status"] = statusNoVariables
			rows = append(rows, base)
		}
		for _, it := range s.Items {
			row := output.Row{}
			for k, v := range base {
				row[k] = v
			}
			row["variable"], row["current"], row["available"] = it.Variable, it.Current, it.Selected
			row["major_available"], row["status"], row["error"] = it.MajorAvailable, it.Status, it.Error
			rows = append(rows, row)
		}
	}
	return rows
}
