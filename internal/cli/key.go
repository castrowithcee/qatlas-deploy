package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

func newKeyCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	key := &cobra.Command{
		Use:   "key",
		Short: "Manage the SSH deploy keys of the stacks repositories",
		Long: "The service creates and keeps the deploy keys itself. The private key never leaves the service; the\n" +
			"public key is entered in the git platform. A git platform accepts a deploy key in one repository\n" +
			"only, so every stack folder has its own key: without --dir the commands manage the key of the folder\n" +
			"bound first, with --dir the key of that folder, also before it is bound. Only root may use the\n" +
			"service channel, so run these commands with sudo.",
	}
	var replace bool
	var dir string
	generate := &cobra.Command{
		Use:   "generate",
		Short: "Generate the Ed25519 deploy key and show its public part",
		Long: "generate creates a new Ed25519 key in the service and prints its public part in authorized_keys\n" +
			"format with the SHA256 fingerprint. An existing key is kept and the command refuses; --replace\n" +
			"replaces it, and the old key stops working at once.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return keyCall(c, program, state, control.KeyGenerateCommand, control.KeyGenerateArgs{Replace: replace, Dir: dir})
		},
	}
	generate.Flags().BoolVar(&replace, "replace", false, "replace an existing deploy key")
	generate.Flags().StringVar(&dir, "dir", "", "stack folder the key is for (absolute path)")
	show := &cobra.Command{
		Use:   "show",
		Short: "Show the public part of the deploy key",
		Long: "show prints the public part of the deploy key in authorized_keys format with its SHA256\n" +
			"fingerprint, for entry in the git platform.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return keyCall(c, program, state, control.KeyShowCommand, control.KeyShowArgs{Dir: dir})
		},
	}
	show.Flags().StringVar(&dir, "dir", "", "stack folder the key is for (absolute path)")
	key.AddCommand(generate, show)
	return key
}

func keyCall(c *cobra.Command, program *cobracli.Program, state *notRunning, command string, args any) error {
	var res control.KeyResult
	if err := call(c.Context(), serviceEndpoint(), command, args, &res); err != nil {
		if errors.Is(err, client.ErrNotRunning) {
			state.seen = true
		}
		return failureOf(err)
	}
	return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
		{Name: "public_key", Value: res.PublicKey},
		{Name: "fingerprint", Value: res.Fingerprint},
	}})
}
