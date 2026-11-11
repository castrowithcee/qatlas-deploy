package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

func newSourceAddCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	var url, fingerprint string
	var gate *cobracli.Gate
	add := &cobra.Command{
		Use:   "add <path>",
		Short: "Take over a folder as a stack folder and give the service the access it needs",
		Long: "add inspects the folder like 'source inspect', shows the case, the answer and the changes it would make,\n" +
			"and applies them only after you confirm: at a terminal by answering the question, without a terminal\n" +
			"with --yes. It never overwrites silently. A folder that is missing or empty is cloned from --url like\n" +
			"'repo set' (and created with owner qdeploy:qdeploy and mode 0755 when missing); a clone with an origin\n" +
			"remote is taken over with that address; a repository without a remote is bound as a local source; a\n" +
			"folder with files and no repository gets a repository with a first commit of the listed files (branch\n" +
			"main, as qdeploy, files left out only in .git/info/exclude, no file written) and is bound as a local\n" +
			"source. If the list changed since it was shown, add stops before it changes anything.\n" +
			"A folder that is rejected is refused and nothing changes.\n" +
			"When the service cannot write the folder or its .git, add gives them to the group of the service account\n" +
			"as root: the group of the folder, of the folders of versioned files and of .git, group write and setgid\n" +
			"on folders, group read and write on versioned files (for a folder of files, the listed files) and on\n" +
			"files in .git, and core.sharedRepository group. The owner never changes. Bind-mount sources of the stacks and everything below, unversioned\n" +
			"files and folders, symbolic links and paths outside the folder stay as they are. The folder is then added\n" +
			"to the systemd drop-in and the service restarts once. --yes does not confirm a server's host key: that\n" +
			"takes the terminal or --fingerprint. Run it with sudo.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageFailure("source add takes exactly one argument, the path of the folder",
					"Run 'qdeploy source add <path> [--url <ssh-url>]'.")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			return runSourceAdd(c, program, gate, state, args[0], url, fingerprint)
		},
	}
	add.Flags().StringVar(&url, "url", "", "SSH address of the repository to clone, or to expect as origin")
	add.Flags().StringVar(&fingerprint, "fingerprint", "",
		"expected SHA256 fingerprint of the server's host key; required without a terminal")
	gate = program.AddGate(add, cobracli.Gates{Question: true})
	return add
}

func runSourceAdd(c *cobra.Command, program *cobracli.Program, gate *cobracli.Gate, state *notRunning, path, url, fingerprint string) error {
	if err := folders.requireRoot(); err != nil {
		return err
	}
	if fingerprint != "" && !strings.HasPrefix(fingerprint, "SHA256:") {
		return usageFailure("--fingerprint must look like SHA256:..., as ssh-keygen -l prints it",
			"Copy the fingerprint of the server's host key.")
	}
	res, err := inspectSource(c, state, path, url)
	if err != nil {
		return err
	}
	notice := c.ErrOrStderr()
	if !structuredFormat(program.Format()) {
		shown := res
		if res.Case == control.SourceCaseFiles {
			// The preview lists the files and what is left out, limited to the first ones.
			shown.PlannedFiles, shown.Excluded = nil, nil
		}
		if err := output.Encode(notice, program.Format(), sourceResult(program.Format(), shown)); err != nil {
			return err
		}
	}
	switch res.Case {
	case control.SourceCaseReject:
		return usageFailure("the folder cannot be taken over: "+res.Reason, cmpFix(res.Fix))
	case control.SourceCaseClone, control.SourceCaseAdopt, control.SourceCaseLocal, control.SourceCaseFiles:
	default:
		return folderFailure("the service answered with the unknown case "+res.Case, nil)
	}

	if res.Case == control.SourceCaseFiles && len(res.PlannedFiles) == 0 {
		return usageFailure("the folder has no file a first commit could take; nothing was changed",
			"Put the stack files into the folder, or look at what is left out with 'qdeploy source inspect "+path+"'.")
	}
	repoURL := res.URL
	if repoURL == "" {
		repoURL = url
	}
	if res.Case != control.SourceCaseLocal && res.Case != control.SourceCaseFiles && repoURL == "" {
		return usageFailure("this folder needs the SSH address of its repository; nothing was changed",
			"Run 'qdeploy source add "+path+" --url <ssh-url>'.")
	}

	var plan *permPlan
	if a := res.Access; res.Case == control.SourceCaseClone && a != nil && a.SetsPermissions && a.ModePath == path {
		_, gid, err := folders.owner()
		if err != nil {
			return folderFailure("cannot find the service account "+serviceAccount, err)
		}
		if plan, err = planFolder(path, gid); err != nil {
			return err
		}
	}
	if res.Case != control.SourceCaseClone {
		if _, err := folders.missing(path); err != nil {
			return err
		}
		a := res.Access
		switch {
		case a == nil:
		case res.Case == control.SourceCaseFiles:
			if a.SetsPermissions {
				_, gid, err := folders.owner()
				if err != nil {
					return folderFailure("cannot find the service account "+serviceAccount, err)
				}
				if plan, err = planListedFiles(path, gid, res.PlannedFiles, bindMounts(res)); err != nil {
					return err
				}
			}
		case a.SetsPermissions || (a.GitWritable != nil && !*a.GitWritable):
			_, gid, err := folders.owner()
			if err != nil {
				return folderFailure("cannot find the service account "+serviceAccount, err)
			}
			if plan, err = planPermissions(c.Context(), path, gid, bindMounts(res)); err != nil {
				return err
			}
		}
	}
	question := previewSourceAdd(c, res, repoURL, plan)
	return gate.Run(cobracli.Request{Question: question}, func() error {
		return executeSourceAdd(c, program, state, res, repoURL, fingerprint, plan)
	})
}

func executeSourceAdd(c *cobra.Command, program *cobracli.Program, state *notRunning, res control.SourceInspectResult,
	repoURL, fingerprint string, plan *permPlan) error {
	path, notice := res.Path, c.ErrOrStderr()
	changed := 0
	if plan != nil {
		changed = plan.changed()
	}
	switch res.Case {
	case control.SourceCaseFiles:
		return executeSourceFiles(c, program, state, res, plan)
	case control.SourceCaseClone, control.SourceCaseAdopt:
		probe, err := trustHostKey(c, state, repoURL, fingerprint)
		if err != nil {
			return err
		}
		if err := applyPlan(c, plan); err != nil {
			return err
		}
		set, err := bindRepo(c, state, repoURL, path, probe, res.Case == control.SourceCaseClone)
		if err != nil {
			return err
		}
		fields := append(repoSetFields(set), output.Field{Name: "changed_paths", Value: changed})
		return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: fields})
	}

	if err := applyPlan(c, plan); err != nil {
		return err
	}
	if err := folders.grant(c.Context(), path, false, notice); err != nil {
		return err
	}
	var added control.SourceAddResult
	if err := credentialCall(c, state, control.SourceAddCommand, control.SourceAddArgs{Path: path}, &added); err != nil {
		return err
	}
	return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
		{Name: "id", Value: added.ID},
		{Name: "path", Value: added.Path},
		{Name: "kind", Value: added.Kind},
		{Name: "head", Value: added.Head},
		{Name: "changed_paths", Value: changed},
	}})
}

// executeSourceFiles starts a repository in a folder of files. It looks at the folder once more first: what
// was added or left out since the preview is not taken along silently, and nothing has changed by then.
func executeSourceFiles(c *cobra.Command, program *cobracli.Program, state *notRunning, shown control.SourceInspectResult, plan *permPlan) error {
	path, notice := shown.Path, c.ErrOrStderr()
	now, err := inspectSource(c, state, path, "")
	if err != nil {
		return err
	}
	if diff := listDifference(shown, now); diff != "" {
		return usageFailure("the folder changed since the list was shown ("+diff+"); nothing was changed",
			"Run 'qdeploy source add "+path+"' again and review the new list.")
	}
	changed := 0
	if plan != nil {
		changed = plan.changed()
	}
	if err := applyPlan(c, plan); err != nil {
		return err
	}
	if err := folders.grant(c.Context(), path, false, notice); err != nil {
		return err
	}
	var added control.SourceAddResult
	if err := credentialCall(c, state, control.SourceAddCommand, control.SourceAddArgs{Path: path, Files: shown.PlannedFiles}, &added); err != nil {
		var f *cli.Failure
		if changed > 0 && errors.As(err, &f) {
			kept := *f
			kept.Message += fmt.Sprintf("; the group and the mode of %d paths of the listed files stay changed", changed)
			return &kept
		}
		return err
	}
	return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
		{Name: "id", Value: added.ID},
		{Name: "path", Value: added.Path},
		{Name: "kind", Value: added.Kind},
		{Name: "head", Value: added.Head},
		{Name: "files", Value: added.Files},
		{Name: "changed_paths", Value: changed},
	}})
}

// listDifference names what differs between two answers for a folder of files: the case, and the paths that
// were added or dropped from the planned files or from the exclusions (path and reason).
func listDifference(shown, now control.SourceInspectResult) string {
	if now.Case != shown.Case {
		return "the folder is now case " + now.Case
	}
	excluded := func(r control.SourceInspectResult) []string {
		var out []string
		for _, e := range r.Excluded {
			out = append(out, e.Path+": "+e.Reason)
		}
		return out
	}
	var parts []string
	for _, set := range []struct {
		name      string
		old, next []string
	}{
		{"files", shown.PlannedFiles, now.PlannedFiles},
		{"left out", excluded(shown), excluded(now)},
	} {
		added, dropped := setDiff(set.old, set.next)
		if len(added) > 0 {
			parts = append(parts, "new in "+set.name+": "+limitedList(added))
		}
		if len(dropped) > 0 {
			parts = append(parts, "no longer in "+set.name+": "+limitedList(dropped))
		}
	}
	return strings.Join(parts, "; ")
}

// setDiff returns the entries of next that old lacks and the entries of old that next lacks, sorted.
func setDiff(old, next []string) (added, dropped []string) {
	in := func(list []string) map[string]bool {
		m := make(map[string]bool, len(list))
		for _, v := range list {
			m[v] = true
		}
		return m
	}
	oldSet, nextSet := in(old), in(next)
	for v := range nextSet {
		if !oldSet[v] {
			added = append(added, v)
		}
	}
	for v := range oldSet {
		if !nextSet[v] {
			dropped = append(dropped, v)
		}
	}
	sort.Strings(added)
	sort.Strings(dropped)
	return added, dropped
}

// listLimit is how many paths a list shows before it only counts the rest.
const listLimit = 50

func limitedList(items []string) string {
	if len(items) <= listLimit {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(items[:listLimit], ", "), len(items)-listLimit)
}

func cmpFix(fix string) string {
	if fix == "" {
		return "Choose another folder."
	}
	return fix
}

func applyPlan(c *cobra.Command, plan *permPlan) error {
	if plan == nil {
		return nil
	}
	if err := folders.applyPermissions(c.Context(), plan); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(c.ErrOrStderr(), "Changed the group and the mode of %d paths in %s.\n", plan.changed(), plan.dir)
	return nil
}

// bindMounts is every bind-mount source of the stacks the folder brings.
func bindMounts(res control.SourceInspectResult) []string {
	var out []string
	for _, st := range res.Stacks {
		out = append(out, st.BindMounts...)
	}
	return out
}

// previewSourceAdd shows what will change and returns the question for the confirmation. Without a terminal
// the confirmation needs --yes; --yes never confirms a host key. Nothing has been changed by then.
func previewSourceAdd(c *cobra.Command, res control.SourceInspectResult, url string, plan *permPlan) string {
	w := c.ErrOrStderr()
	var lines []string
	switch res.Case {
	case control.SourceCaseClone:
		lines = append(lines, fmt.Sprintf("Clone %s into %s; a missing folder is created with owner %s:%s and mode %04o.",
			url, res.Path, serviceAccount, serviceAccount, folderMode))
	case control.SourceCaseAdopt:
		lines = append(lines, fmt.Sprintf("Take over the clone %s with the address %s.", res.Path, url))
	case control.SourceCaseFiles:
		lines = append(lines, filesPreview(res)...)
	default:
		lines = append(lines, fmt.Sprintf("Take over the repository %s as a local source.", res.Path))
	}
	if plan != nil {
		lines = append(lines, plan.summary()...)
	}
	lines = append(lines, fmt.Sprintf("The folder is added to %s and the %s service restarts.", folders.dropIn(), serviceAccount))
	_, _ = fmt.Fprintf(w, "Case %s, folder %s:\n", res.Case, res.Path)
	for _, l := range lines {
		_, _ = fmt.Fprintln(w, "  "+l)
	}
	return fmt.Sprintf("Apply these changes (case %s, folder %s)?", res.Case, res.Path)
}

// filesPreview describes the repository a folder of files gets: the files of the first commit, what is left
// out with the reason, and where each part is recorded.
func filesPreview(res control.SourceInspectResult) []string {
	lines := []string{
		fmt.Sprintf("Start a git repository in %s (branch main, core.sharedRepository=group) and bind it as a local source.", res.Path),
		"The first commit is made as " + serviceAccount + "; no file in the folder is written.",
		fmt.Sprintf("The first commit takes %d files:", len(res.PlannedFiles)),
	}
	for i, f := range res.PlannedFiles {
		if i == listLimit {
			lines = append(lines, fmt.Sprintf("  ... and %d more", len(res.PlannedFiles)-listLimit))
			break
		}
		lines = append(lines, "  "+f)
	}
	if len(res.Excluded) == 0 {
		return append(lines, "Nothing is left out.")
	}
	lines = append(lines, fmt.Sprintf("Left out (%d), recorded only in .git/info/exclude, never in .gitignore:", len(res.Excluded)))
	for i, e := range res.Excluded {
		if i == listLimit {
			lines = append(lines, fmt.Sprintf("  ... and %d more", len(res.Excluded)-listLimit))
			break
		}
		line := "  " + e.Path + ": " + e.Reason
		if e.Stack != "" {
			line += " (stack " + e.Stack + ")"
		}
		lines = append(lines, line)
	}
	return lines
}
