package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/client"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
)

func newKeyCommand(program *cobracli.Program, state *notRunning, secrets *redact.Redactor) *cobra.Command {
	key := &cobra.Command{
		Use:   "key",
		Short: "Manage the SSH deploy keys of the stacks repositories",
		Long: "The service keeps the deploy keys. It creates one itself (generate) or stores one you already have, for\n" +
			"example from a password manager (set). The private key never leaves the service; the public key is\n" +
			"entered in the git platform. A git platform accepts a deploy key in one repository\n" +
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
	var file string
	set := &cobra.Command{
		Use:   "set",
		Short: "Store an existing SSH private key as the deploy key",
		Long: "set stores a private key you already have as the deploy key and prints its public part in\n" +
			"authorized_keys format with the SHA256 fingerprint. The key is read without echo when standard\n" +
			"input is a terminal (paste it, it ends with the END line), otherwise from standard input; --file\n" +
			"reads it from a file instead. The key is never an argument. It must be an OpenSSH private key\n" +
			"(BEGIN OPENSSH PRIVATE KEY) without passphrase, Ed25519 or RSA of at least 3072 bits, at most\n" +
			"16 KiB. An existing key is kept and the command refuses; --replace replaces it, and the old key\n" +
			"stops working at once.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			text, err := readKey(c, file)
			if err != nil {
				return err
			}
			secrets.Add(text)
			return keyCall(c, program, state, control.KeySetCommand, control.KeySetArgs{Key: text, Replace: replace, Dir: dir})
		},
	}
	set.Flags().BoolVar(&replace, "replace", false, "replace an existing deploy key")
	set.Flags().StringVar(&dir, "dir", "", "stack folder the key is for (absolute path)")
	set.Flags().StringVar(&file, "file", "", "read the private key from this file instead of the terminal or stdin")
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
	key.AddCommand(generate, set, show)
	return key
}

func keyCall(c *cobra.Command, program *cobracli.Program, state *notRunning, command string, args any) error {
	var res control.KeyResult
	if err := call(c.Context(), serviceEndpoint(), command, args, &res); err != nil {
		if errors.Is(err, client.ErrNotRunning) {
			state.seen = true
		}
		f := failureOf(err)
		var fail *cli.Failure
		if command == control.KeySetCommand && errors.As(f, &fail) && fail.Code == output.CodeConfirmationRequired {
			fail.NextStep = "Run 'qdeploy key set --replace' to replace it; the old key stops working at once."
		}
		return f
	}
	return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
		{Name: "public_key", Value: res.PublicKey},
		{Name: "fingerprint", Value: res.Fingerprint},
	}})
}

// readKey reads the private key from file, from the terminal without echo, or from standard input. Its errors
// never carry key text.
func readKey(c *cobra.Command, file string) (string, error) {
	var raw []byte
	var err error
	switch in := stdinStream(); {
	case file != "":
		raw, err = readKeyFile(file)
	default:
		if f, ok := in.(*os.File); ok && isTerminal(f) {
			raw, err = readKeyHidden(c, f)
		} else {
			raw, err = io.ReadAll(io.LimitReader(in, credentials.MaxKeyBytes+1))
		}
	}
	if err != nil {
		var f *cli.Failure
		if errors.As(err, &f) {
			return "", err
		}
		return "", &cli.Failure{Code: output.CodeRuntime, Message: "cannot read the key",
			NextStep: "Run the command again, or give the key with --file or on standard input."}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", usageFailure("no key was provided", "Paste the key at the prompt, pipe it on standard input, or use --file.")
	}
	if len(raw) > credentials.MaxKeyBytes {
		return "", usageFailure(credentials.ErrKeyTooLarge.Error(), "Provide a smaller key and run the command again.")
	}
	return string(raw), nil
}

func readKeyFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, &cli.Failure{Code: output.CodeUsage, Message: "cannot open the key file", Usage: true,
			NextStep: "Check the path given with --file and run the command again."}
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, credentials.MaxKeyBytes+1))
}

// readKeyHidden reads pasted lines without echo up to the END line of a PEM block, or up to the end of input.
func readKeyHidden(c *cobra.Command, f *os.File) ([]byte, error) {
	_, _ = fmt.Fprint(c.ErrOrStderr(), "Paste the private key (input is hidden; it ends with the END line): ")
	defer func() { _, _ = fmt.Fprintln(c.ErrOrStderr()) }()
	var buf bytes.Buffer
	for buf.Len() <= credentials.MaxKeyBytes {
		line, err := readHidden(f)
		if len(line) > 0 || err == nil {
			buf.Write(line)
			buf.WriteByte('\n')
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if bytes.HasPrefix(bytes.TrimSpace(line), []byte("-----END ")) {
			break
		}
	}
	return buf.Bytes(), nil
}
