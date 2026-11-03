package cli

import (
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

// The status of a changelog row besides "release", which is one release of the range.
const (
	statusNoNotes   = "no-release-notes"
	statusNoUpdates = "no-updates"
)

// maxNotesRunes bounds the one-line notes of the table and compact formats.
const maxNotesRunes = 100

func newChangelogCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	return &cobra.Command{
		Use:   "changelog <stack>",
		Short: "Show the release notes between the current and the available version of a stack",
		Long: "changelog lists, for every image version variable of the stack with an available update, the GitHub\n" +
			"release notes after the current version up to and including the available one. The repository comes\n" +
			"from update.changelog in the stack's qdeploy.yaml or else from the image label\n" +
			"org.opencontainers.image.source. If no notes can be read, the row says why and shows a link instead;\n" +
			"this never affects a deployment. Only the stored update check is used (see qdeploy stack updates --refresh).\n" +
			"The table and compact formats show the first line of each release's notes; json and toon carry the\n" +
			"full text as body. A GitHub token may be stored as the credential github.com (see qdeploy help\n" +
			"credentials).",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.ChangelogResult
			err := callFor(c.Context(), control.ChangelogTimeout+client.CallTimeout, serviceEndpoint(),
				control.ChangelogShowCommand, control.ChangelogArgs{Stack: args[0]}, &res)
			if err != nil {
				if errors.Is(err, client.ErrNotRunning) {
					state.seen = true
				}
				return failureOf(err)
			}
			return encodeChangelogs(c.OutOrStdout(), program.Format(),
				[]stackChangelog{{name: res.Stack, checked: res.Checked, items: res.Items}})
		},
	}
}

type stackChangelog struct {
	name    string
	checked time.Time
	err     string
	items   []control.ChangelogInfo
}

func changelogsOfUpdates(res control.UpdatesResult) []stackChangelog {
	out := make([]stackChangelog, 0, len(res.Stacks))
	for _, s := range res.Stacks {
		out = append(out, stackChangelog{name: s.Name, checked: s.Checked, err: s.Error, items: s.Changelogs})
	}
	return out
}

var changelogColumns = []string{"stack", "variable", "current", "target", "status", "version", "published", "name", "url", "source", "link", "reason", "omitted"}

func encodeChangelogs(w io.Writer, format output.Format, list []stackChangelog) error {
	columns := append([]string(nil), changelogColumns...)
	if format == output.FormatJSON || format == output.FormatTOON {
		columns = append(columns, "body")
	} else {
		columns = append(columns, "notes")
	}
	return output.Encode(w, format, output.Collection{Columns: columns, Rows: changelogRows(list)})
}

func changelogRows(list []stackChangelog) []output.Row {
	var rows []output.Row
	for _, s := range list {
		switch {
		case s.err != "":
			rows = append(rows, output.Row{"stack": s.name, "status": statusUnreadable, "reason": s.err})
			continue
		case s.checked.IsZero():
			rows = append(rows, output.Row{"stack": s.name, "status": statusNeverChecked})
			continue
		case len(s.items) == 0:
			rows = append(rows, output.Row{"stack": s.name, "status": statusNoUpdates})
			continue
		}
		for _, it := range s.items {
			base := output.Row{"stack": s.name, "variable": it.Variable, "current": it.Current, "target": it.Target,
				"source": it.Source}
			if len(it.Releases) == 0 {
				base["status"], base["link"], base["reason"] = statusNoNotes, it.Link, it.Reason
				if it.Omitted > 0 {
					base["omitted"] = it.Omitted
				}
				rows = append(rows, base)
				continue
			}
			for i, r := range it.Releases {
				row := output.Row{}
				for k, v := range base {
					row[k] = v
				}
				row["status"], row["version"], row["name"], row["url"] = "release", r.Version, r.Name, r.URL
				if !r.Published.IsZero() {
					row["published"] = r.Published.UTC().Format(time.RFC3339)
				}
				row["body"], row["notes"] = r.Body, firstLine(r.Body)
				if i == len(it.Releases)-1 {
					row["link"] = it.Link
					if it.Omitted > 0 {
						row["omitted"] = it.Omitted
					}
				}
				rows = append(rows, row)
			}
		}
	}
	return rows
}

// firstLine is the first non-empty line of s, cut to maxNotesRunes.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > maxNotesRunes {
			line = string([]rune(line)[:maxNotesRunes]) + "..."
		}
		return line
	}
	return ""
}
