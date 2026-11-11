package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
)

func newSourceRemoteCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	var fingerprint string
	var gate *cobracli.Gate
	remote := &cobra.Command{
		Use:   "remote <path> <ssh-url>",
		Short: "Give a local source a remote repository, after comparing the folder with it",
		Long: "remote compares the history of a local source with the repository at the address and shows how they\n" +
			"relate: the remote has no commit (the local history is pushed to it), it holds the same files (the\n" +
			"service follows its history; the local history stays under " + gitrepo.LocalHistoryRef + "), or it\n" +
			"differs (the paths are listed; the state of the host becomes one commit on the remote branch and is\n" +
			"pushed, and the host never overwrites the remote). Nothing is changed before you confirm: at a\n" +
			"terminal by answering the question, without a terminal with --yes, which does not confirm a server's\n" +
			"host key (that takes the terminal or --fingerprint). If the commits that would be pushed hold secret-like\n" +
			"files (secrets files, env files with values, private keys), remote stops, names commit, path and kind\n" +
			"and never a value; there is no way around it, clean the history yourself. The source is then a remote\n" +
			"source. The deploy key of the folder needs write access to the repository. Run it with sudo.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return usageFailure("source remote takes two arguments, the path of the folder and the SSH address",
					"Run 'qdeploy source remote <path> <ssh-url>'.")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			return runSourceRemote(c, program, gate, state, args[0], args[1], fingerprint)
		},
	}
	remote.Flags().StringVar(&fingerprint, "fingerprint", "",
		"expected SHA256 fingerprint of the server's host key; required without a terminal")
	gate = program.AddGate(remote, cobracli.Gates{Question: true})
	return remote
}

func runSourceRemote(c *cobra.Command, program *cobracli.Program, gate *cobracli.Gate, state *notRunning, path, url, fingerprint string) error {
	if err := folders.requireRoot(); err != nil {
		return err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return usageFailure("the path must be absolute and clean", "Pass the absolute path of the folder.")
	}
	if err := gitrepo.ValidateURL(url); err != nil {
		return usageFailure(err.Error(), "Pass the SSH address of the repository.")
	}
	if fingerprint != "" && !strings.HasPrefix(fingerprint, "SHA256:") {
		return usageFailure("--fingerprint must look like SHA256:..., as ssh-keygen -l prints it",
			"Copy the fingerprint of the server's host key.")
	}
	probe, err := trustHostKey(c, state, url, fingerprint)
	if err != nil {
		return err
	}
	cmp, err := compareRemote(c, state, path, url, probe.Fingerprint)
	if err != nil {
		return err
	}
	if cmp.Case != control.SourceCaseLocal {
		return usageFailure("source remote works on a local source; this folder is case "+cmp.Case,
			"Run 'qdeploy source add "+path+" --url <ssh-url>' for a folder of files.")
	}
	w := c.ErrOrStderr()
	_, _ = fmt.Fprintf(w, "Compare %s with %s:\n", path, url)
	printLines(w, compareLines(cmp))
	if err := refuseSecrets(cmp); err != nil {
		return err
	}
	question, err := remoteQuestion(cmp, url)
	if err != nil {
		return err
	}
	return gate.Run(cobracli.Request{Question: question}, func() error {
		var res control.SourceRemoteResult
		args := control.SourceRemoteArgs{Path: path, URL: url, Fingerprint: probe.Fingerprint,
			ExpectState: cmp.State, ExpectCommit: cmp.Commit}
		if err := repoCall(c, state, control.SourceRemoteCommand, args, &res); err != nil {
			return err
		}
		return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
			{Name: "id", Value: res.ID},
			{Name: "path", Value: res.Path},
			{Name: "url", Value: res.URL},
			{Name: "state", Value: res.State},
			{Name: "branch", Value: res.Branch},
			{Name: "head", Value: res.Head},
			{Name: "pushed", Value: res.Pushed},
			{Name: "history_ref", Value: res.HistoryRef},
		}})
	})
}

func compareRemote(c *cobra.Command, state *notRunning, path, url, fingerprint string) (control.SourceCompareResult, error) {
	var cmp control.SourceCompareResult
	err := repoCall(c, state, control.SourceCompareCommand,
		control.SourceCompareArgs{Path: path, URL: url, Fingerprint: fingerprint}, &cmp)
	return cmp, err
}

func printLines(w io.Writer, lines []string) {
	for _, l := range lines {
		_, _ = fmt.Fprintln(w, "  "+l)
	}
}

// shortCommit is the part of a commit id the output shows.
func shortCommit(id string) string { return id[:min(len(id), 12)] }

// compareLines describes how the remote relates to the folder: the state, the paths that differ and the
// secret-like files a push would publish. Paths and kinds only; no value is ever shown.
func compareLines(cmp control.SourceCompareResult) []string {
	remote := "branch " + cmp.Branch
	if cmp.Commit != "" {
		remote += " at " + shortCommit(cmp.Commit)
	}
	lines := []string{fmt.Sprintf("State %s (remote %s).", cmp.State, remote)}
	if cmp.PathCount > 0 {
		lines = append(lines, fmt.Sprintf("%d paths differ between the remote and the folder:", cmp.PathCount))
		for i, p := range cmp.Paths {
			if i == listLimit {
				break
			}
			lines = append(lines, "  "+p)
		}
		if shown := min(len(cmp.Paths), listLimit); cmp.PathCount > shown {
			lines = append(lines, fmt.Sprintf("  ... and %d more", cmp.PathCount-shown))
		}
	}
	if s := cmp.Secrets; s != nil && s.Total > 0 {
		lines = append(lines, fmt.Sprintf("The commits to push hold %d secret-like files:", s.Total))
		for i, f := range s.Findings {
			if i == listLimit {
				break
			}
			lines = append(lines, fmt.Sprintf("  %s %s (%s)", shortCommit(f.Commit), f.Path, f.Kind))
		}
		if shown := min(len(s.Findings), listLimit); s.Total > shown {
			lines = append(lines, fmt.Sprintf("  ... and %d more", s.Total-shown))
		}
	}
	return lines
}

// refuseSecrets stops before any change when the push would publish secret-like files. There is no override.
func refuseSecrets(cmp control.SourceCompareResult) error {
	if cmp.Secrets == nil || cmp.Secrets.Total == 0 {
		return nil
	}
	return secretsFailure(fmt.Sprintf("the commits to push hold %d secret-like files and are not pushed; nothing was changed",
		cmp.Secrets.Total))
}

func secretsFailure(message string) *cli.Failure {
	return &cli.Failure{Code: output.Code(control.CodeSecrets), Message: message,
		NextStep: "qdeploy cannot push these files and has no override. Remove them from the history yourself (rewrite " +
			"it, or start a new history without them), then run the command again."}
}

// remoteQuestion is what the operator confirms for a local source and the state of its remote.
func remoteQuestion(cmp control.SourceCompareResult, url string) (string, error) {
	switch cmp.State {
	case control.SourceRemoteEmpty:
		return fmt.Sprintf("Push the local history to %s (branch %s)?", url, cmp.Branch), nil
	case control.SourceRemoteEqual:
		return fmt.Sprintf("Take over the history of the remote; the local history stays under %s?", gitrepo.LocalHistoryRef), nil
	case control.SourceRemoteDifferent:
		return fmt.Sprintf("Put the state of the host as a commit on %s and push it?", cmp.Branch), nil
	}
	return "", folderFailure("the service answered with the unknown state "+cmp.State, nil)
}
