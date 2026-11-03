package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
)

// DefaultStacksDir is where the working copy of the stacks repository lives unless --dir says otherwise.
const DefaultStacksDir = "/opt/stacks"

// remoteCallLimit bounds the commands that wait on the git server (a clone or a fetch).
const remoteCallLimit = 3 * time.Minute

func newRepoCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	group := &cobra.Command{
		Use:   "repo",
		Short: "Bind and update the stacks repository",
		Long: "The service clones the stacks repository over SSH with its own deploy key and keeps the server's\n" +
			"host key in its own known_hosts file. Only root may use the service channel, so run these commands\n" +
			"with sudo.",
	}
	var dir, fingerprint string
	set := &cobra.Command{
		Use:   "set <ssh-url>",
		Short: "Bind the stacks repository, confirm its host key and clone it",
		Long: "set shows the SSH host key of the server with its SHA256 fingerprint the first time and stores it\n" +
			"only after you confirm it: at a terminal by answering the question, without a terminal by passing\n" +
			"the expected fingerprint with --fingerprint. Then the service clones the repository into --dir\n" +
			"(default " + DefaultStacksDir + ") with its deploy key. The URL is user@host:path or\n" +
			"ssh://[user@]host[:port]/path. The directory must be an absolute path that does not exist, is empty,\n" +
			"or already is a clone of this URL. A host that is pinned with another key is refused and the pinned\n" +
			"key is never replaced; a repository bound earlier is not replaced either.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageFailure("repo set takes exactly one argument, the SSH URL of the stacks repository",
					"Run 'qdeploy repo set <ssh-url> [--dir <path>]'.")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			return runRepoSet(c, program, state, args[0], dir, fingerprint)
		},
	}
	set.Flags().StringVar(&dir, "dir", DefaultStacksDir, "directory of the working copy (absolute path)")
	set.Flags().StringVar(&fingerprint, "fingerprint", "",
		"expected SHA256 fingerprint of the server's host key; required without a terminal")
	fetch := &cobra.Command{
		Use:   "fetch",
		Short: "Fetch new commits of the stacks repository",
		Long: "fetch updates the remote-tracking branch of the bound repository and reports whether it differs from\n" +
			"the working copy. The working copy itself is not changed. A host key that differs from the pinned one\n" +
			"aborts the command.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var res control.RepoFetchResult
			if err := repoCall(c, state, control.RepoFetchCommand, nil, &res); err != nil {
				return err
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
				{Name: "dir", Value: res.Dir},
				{Name: "head", Value: res.Head},
				{Name: "remote_head", Value: res.RemoteHead},
				{Name: "updates_available", Value: res.UpdatesAvailable},
			}})
		},
	}
	group.AddCommand(set, fetch)
	return group
}

func runRepoSet(c *cobra.Command, program *cobracli.Program, state *notRunning, url, dir, fingerprint string) error {
	if err := gitrepo.ValidateURL(url); err != nil {
		return usageFailure(err.Error(), "Pass the SSH URL of the stacks repository.")
	}
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return usageFailure("--dir must be an absolute, clean path", "Pass an absolute directory such as "+DefaultStacksDir+".")
	}
	if fingerprint != "" && !strings.HasPrefix(fingerprint, "SHA256:") {
		return usageFailure("--fingerprint must look like SHA256:..., as ssh-keygen -l prints it",
			"Copy the fingerprint of the server's host key.")
	}

	var probe control.RepoProbeResult
	if err := repoCall(c, state, control.RepoProbeCommand, control.RepoProbeArgs{URL: url}, &probe); err != nil {
		return err
	}
	target := probe.Host
	if probe.Port != "22" {
		target = fmt.Sprintf("%s:%s", probe.Host, probe.Port)
	}
	switch {
	case fingerprint != "":
		if fingerprint != probe.Fingerprint {
			return &cli.Failure{Code: output.CodeAuth, Message: fmt.Sprintf(
				"the host key of %s has fingerprint %s, not the expected %s; nothing was stored",
				target, probe.Fingerprint, fingerprint),
				NextStep: "Do not continue unless the difference is explained and verified out of band."}
		}
	case probe.Pinned:
		// The operator confirmed exactly this key before.
	default:
		if err := confirmHostKey(c, target, probe); err != nil {
			return err
		}
	}

	var res control.RepoSetResult
	args := control.RepoSetArgs{URL: url, Dir: dir, Fingerprint: probe.Fingerprint}
	if err := repoCall(c, state, control.RepoSetCommand, args, &res); err != nil {
		return err
	}
	return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
		{Name: "url", Value: res.URL},
		{Name: "dir", Value: res.Dir},
		{Name: "host_key_fingerprint", Value: res.Fingerprint},
		{Name: "head", Value: res.Head},
		{Name: "cloned", Value: res.Cloned},
	}})
}

// confirmHostKey shows the key and asks at a terminal; without a terminal it refuses and names the flag.
func confirmHostKey(c *cobra.Command, target string, probe control.RepoProbeResult) error {
	in := stdinStream()
	f, ok := in.(*os.File)
	if !ok || !isTerminal(f) {
		return &cli.Failure{Code: output.CodeConfirmationRequired, Usage: true,
			Message: fmt.Sprintf("the host key of %s (%s) has fingerprint %s and is not trusted yet",
				target, probe.KeyType, probe.Fingerprint),
			NextStep: "Compare the fingerprint with the git platform, then run the command again with --fingerprint " +
				probe.Fingerprint + "."}
	}
	w := c.ErrOrStderr()
	_, _ = fmt.Fprintf(w, "Host key of %s\n  type:        %s\n  fingerprint: %s\n", target, probe.KeyType, probe.Fingerprint)
	_, _ = fmt.Fprint(w, "Compare the fingerprint with the git platform. Trust this key? [y/N] ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return &cli.Failure{Code: output.CodeRuntime, Message: "cannot read the answer from the terminal",
			NextStep: "Run the command again.", Cause: err}
	}
	if answer := strings.ToLower(strings.TrimSpace(line)); answer != "y" && answer != "yes" {
		return &cli.Failure{Code: output.CodeConfirmationDeclined, Message: "the host key was not confirmed; nothing was stored",
			NextStep: "Run the command again when the fingerprint is verified."}
	}
	return nil
}

func repoCall(c *cobra.Command, state *notRunning, command string, args, result any) error {
	return remoteCall(c, state, remoteCallLimit, command, args, result)
}

func remoteCall(c *cobra.Command, state *notRunning, limit time.Duration, command string, args, result any) error {
	if err := callFor(c.Context(), limit, serviceEndpoint(), command, args, result); err != nil {
		if errors.Is(err, client.ErrNotRunning) {
			state.seen = true
		}
		return failureOf(err)
	}
	return nil
}
