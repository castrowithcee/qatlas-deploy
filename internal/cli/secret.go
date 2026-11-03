package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

func newSecretCommand(program *cobracli.Program, state *notRunning, secrets *redact.Redactor) *cobra.Command {
	group := &cobra.Command{
		Use:   "secret",
		Short: "Set, list and delete the secrets of a stack",
		Long: "The secrets of a stack live in its secrets.env, which exists on this host only and is never versioned.\n" +
			"A key stands in secrets.env or in .env, never in both. A change does not redeploy the stack: it applies\n" +
			"at the next deployment, so run 'qdeploy stack deploy <stack>' afterwards. Values are never shown or\n" +
			"logged. Only root may use the service channel, so run these commands with sudo.",
	}
	set := &cobra.Command{
		Use:   "set <stack> <KEY>",
		Short: "Write a secret read from the terminal or stdin to secrets.env",
		Long: "set reads the value without echo when standard input is a terminal, and otherwise from standard\n" +
			"input up to the end of input, dropping one trailing line end. The value is never an argument: it\n" +
			"would show in the process list and the shell history. A key has 1 to 128 characters: letters, digits,\n" +
			"'_', '.' or '-', starting with a letter or '_'. A value has 4 to 8192 bytes of text without control\n" +
			"characters; a value with a single quote must not contain '$'. The file is created with mode 0600 if it\n" +
			"does not exist, and an existing key is replaced. A key that already stands in .env is refused. The\n" +
			"command waits for a run of the stack to end.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return usageFailure("secret set takes exactly two arguments, the stack and the key; the value is never accepted as an argument",
					"Run 'qdeploy stack secret set <stack> <KEY>' and enter the value at the prompt or pipe it on standard input.")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			stack, key := args[0], args[1]
			if !stacks.ValidSecretKey(key) {
				return usageFailure(stacks.ErrInvalidSecretKey.Error(), "Choose another key and run the command again.")
			}
			value, err := readValue(c, key)
			if err != nil {
				return err
			}
			secrets.Add(value)
			var res control.SecretResult
			if err := secretCall(c, state, control.SecretSetCommand,
				control.SecretSetArgs{Stack: stack, Key: key, Value: value}, &res); err != nil {
				return err
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
				{Name: "stack", Value: res.Stack}, {Name: "key", Value: res.Key}, {Name: "stored", Value: true},
				{Name: "replaced", Value: res.Replaced},
			}})
		},
	}
	list := &cobra.Command{
		Use:   "list <stack>",
		Short: "List the secret keys of a stack",
		Long:  "list shows the keys in the stack's secrets.env, never their values.",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.SecretListResult
			if err := secretCall(c, state, control.SecretListCommand, control.SecretListArgs{Stack: args[0]}, &res); err != nil {
				return err
			}
			rows := make([]output.Row, 0, len(res.Keys))
			for _, k := range res.Keys {
				rows = append(rows, output.Row{"key": k})
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Collection{Columns: []string{"key"}, Rows: rows})
		},
	}
	del := &cobra.Command{
		Use:   "delete <stack> <KEY>",
		Short: "Remove a secret from secrets.env",
		Long: "delete removes the key from the stack's secrets.env and keeps the rest of the file. The stack is not\n" +
			"redeployed; a deployment without a key the Compose files need fails.",
		Args: cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			if !stacks.ValidSecretKey(args[1]) {
				return usageFailure(stacks.ErrInvalidSecretKey.Error(), "Check the keys with 'qdeploy stack secret list "+args[0]+"'.")
			}
			var res control.SecretResult
			if err := secretCall(c, state, control.SecretDeleteCommand,
				control.SecretKeyArgs{Stack: args[0], Key: args[1]}, &res); err != nil {
				return err
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
				{Name: "stack", Value: res.Stack}, {Name: "key", Value: res.Key}, {Name: "deleted", Value: true},
			}})
		},
	}
	group.AddCommand(set, list, del)
	return group
}

// secretCall answers a key that stands in .env itself: the generic mapping of key-exists belongs to the
// service key.
func secretCall(c *cobra.Command, state *notRunning, command string, args, result any) error {
	err := credentialCall(c, state, command, args, result)
	var f *cli.Failure
	if errors.As(err, &f) && f.Code == output.CodeConfirmationRequired && f.Usage {
		var remote *control.Error
		if errors.As(f.Cause, &remote) && remote.Code == control.CodeKeyExists {
			return &cli.Failure{Code: output.CodeInvalidRequest, Message: remote.Message, Usage: true,
				NextStep: "Keep the key in one file: remove it from .env in the stacks repository, or choose another key.", Cause: f.Cause}
		}
	}
	return err
}
