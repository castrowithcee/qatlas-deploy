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
		Short: "Manage the SSH deploy key of the stacks repository",
		Long: "The service creates and keeps the deploy key itself. The private key never leaves the service; the\n" +
			"public key is entered in the git platform. Only root may use the service channel, so run these\n" +
			"commands with sudo.",
	}
	var replace bool
	generate := &cobra.Command{
		Use:   "generate",
		Short: "Generate the Ed25519 deploy key and show its public part",
		Long: "generate creates a new Ed25519 key in the service and prints its public part in authorized_keys\n" +
			"format with the SHA256 fingerprint. An existing key is kept and the command refuses; --replace\n" +
			"replaces it, and the old key stops working at once.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return keyCall(c, program, state, control.KeyGenerateCommand, control.KeyGenerateArgs{Replace: replace})
		},
	}
	generate.Flags().BoolVar(&replace, "replace", false, "replace an existing deploy key")
	show := &cobra.Command{
		Use:   "show",
		Short: "Show the public part of the deploy key",
		Long: "show prints the public part of the deploy key in authorized_keys format with its SHA256\n" +
			"fingerprint, for entry in the git platform.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return keyCall(c, program, state, control.KeyShowCommand, nil)
		},
	}
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
