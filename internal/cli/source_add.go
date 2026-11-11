package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

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
			"remote is taken over with that address; a repository without a remote is bound as a local source.\n" +
			"A folder that is rejected, and a folder with files and no repository, is refused and nothing changes.\n" +
			"When the service cannot write the folder or its .git, add gives them to the group of the service account\n" +
			"as root: the group of the folder, of the folders of versioned files and of .git, group write and setgid\n" +
			"on folders, group read and write on versioned files and on files in .git, and core.sharedRepository\n" +
			"group. The owner never changes. Bind-mount sources of the stacks and everything below, unversioned\n" +
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
		if err := output.Encode(notice, program.Format(), sourceResult(program.Format(), res)); err != nil {
			return err
		}
	}
	switch res.Case {
	case control.SourceCaseReject:
		return usageFailure("the folder cannot be taken over: "+res.Reason, cmpFix(res.Fix))
	case control.SourceCaseFiles:
		return usageFailure("the folder holds files and no repository, and source add cannot take files without git yet; nothing was changed",
			"Make the folder a git repository (git init, git add, git commit) and run source add again.")
	case control.SourceCaseClone, control.SourceCaseAdopt, control.SourceCaseLocal:
	default:
		return folderFailure("the service answered with the unknown case "+res.Case, nil)
	}

	repoURL := res.URL
	if repoURL == "" {
		repoURL = url
	}
	if res.Case != control.SourceCaseLocal && repoURL == "" {
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
		if a := res.Access; a != nil && (a.SetsPermissions || (a.GitWritable != nil && !*a.GitWritable)) {
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
