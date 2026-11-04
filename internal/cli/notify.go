package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
)

func newNotifyCommand(program *cobracli.Program, state *notRunning, secrets *redact.Redactor) *cobra.Command {
	group := &cobra.Command{
		Use:   "notify",
		Short: "Manage the notification channels of the host",
		Long: "A channel receives the events it selected: a webhook, a Slack channel, a Telegram chat or an e-mail\n" +
			"address. The parts that grant the right to send are stored as credentials by the service and never shown\n" +
			"again. Only root may use the service channel, so run these commands with sudo. See 'qdeploy help notifications'.",
	}
	group.AddCommand(newNotifyList(program, state), newNotifyAdd(program, state, secrets),
		newNotifyRemove(program, state), newNotifyTest(program, state))
	return group
}

func newNotifyList(program *cobracli.Program, state *notRunning) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the notification channels",
		Long:  "list shows each channel with its type, its events and its settings, never its secret parts.",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var res control.NotifyListResult
			if err := notifyCall(c, state, control.NotifyListCommand, nil, &res); err != nil {
				return err
			}
			rows := make([]output.Row, 0, len(res.Channels))
			for _, ch := range res.Channels {
				rows = append(rows, output.Row{"name": ch.Name, "type": ch.Type,
					"events": strings.Join(ch.Events, ","), "settings": channelSettings(ch)})
			}
			return output.Encode(c.OutOrStdout(), program.Format(),
				output.Collection{Columns: []string{"name", "type", "events", "settings"}, Rows: rows})
		},
	}
}

func newNotifyAdd(program *cobracli.Program, state *notRunning, secrets *redact.Redactor) *cobra.Command {
	var a control.NotifyAddArgs
	add := &cobra.Command{
		Use:   "add <name> --type webhook|slack|telegram|smtp [--events <list>] [type settings]",
		Short: "Add a notification channel; its secret parts are read from the terminal or stdin",
		Long: "add stores a channel. A name has 1 to 32 characters: lowercase letters, digits or '-', starting with a\n" +
			"letter. --events takes a comma list of succeeded, failed, rolled-back, rollback-failed,\n" +
			"container-failed, recovered and update-available; without it the channel receives all of them.\n\n" +
			"Settings by type: webhook and slack take none; telegram takes --chat-id and optionally --base-url (the\n" +
			"address of a Bot API server); smtp takes --host, --from and --to (a comma list or repeated), optionally\n" +
			"--port, --security starttls (default) or tls, and --user.\n\n" +
			"The secret parts are never an argument or a flag value. At a terminal each is asked for without echo.\n" +
			"Piped on standard input, they are lines in this order, the last without a line end being fine:\n" +
			"  webhook   line 1: the https URL, line 2: the signing key\n" +
			"  slack     the incoming-webhook URL\n" +
			"  telegram  the bot token\n" +
			"  smtp      the password, only with --user; without --user nothing is read\n" +
			"A secret has 4 to 8192 bytes of text without control characters.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageFailure("notify add takes exactly one argument, the channel name; secret parts are never accepted as arguments",
					"Run 'qdeploy notify add <name> --type <type>' and enter the secret at the prompt or pipe it on standard input.")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			a.Name = args[0]
			prompts := secretPrompts(a)
			values, err := readSecrets(c, a.Name, prompts)
			if err != nil {
				return err
			}
			for _, v := range values {
				secrets.Add(v)
			}
			switch len(values) {
			case 2:
				a.Secret, a.Key = values[0], values[1]
			case 1:
				a.Secret = values[0]
			}
			var res control.NotifyChannelInfo
			if err := notifyCall(c, state, control.NotifyAddCommand, a, &res); err != nil {
				return err
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: channelFields(res)})
		},
	}
	f := add.Flags()
	f.StringVar(&a.Type, "type", "", "channel type: webhook, slack, telegram or smtp")
	f.StringSliceVar(&a.Events, "events", nil, "events the channel receives, comma separated; default all")
	f.StringVar(&a.Host, "host", "", "smtp: server host name")
	f.IntVar(&a.Port, "port", 0, "smtp: server port; default 587 with starttls, 465 with tls")
	f.StringVar(&a.Security, "security", "", "smtp: starttls (default) or tls")
	f.StringVar(&a.User, "user", "", "smtp: login name; the password is then read like any secret")
	f.StringVar(&a.From, "from", "", "smtp: sender address")
	f.StringSliceVar(&a.To, "to", nil, "smtp: recipient addresses, comma separated or repeated")
	f.StringVar(&a.ChatID, "chat-id", "", "telegram: chat to write to")
	f.StringVar(&a.BaseURL, "base-url", "", "telegram: address of a Bot API server instead of the public one")
	return add
}

func newNotifyRemove(program *cobracli.Program, state *notRunning) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a notification channel and its stored secrets",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := notifyCall(c, state, control.NotifyRemoveCommand, control.NotifyNameArgs{Name: args[0]}, nil); err != nil {
				return err
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
				{Name: "name", Value: args[0]}, {Name: "removed", Value: true},
			}})
		},
	}
}

func newNotifyTest(program *cobracli.Program, state *notRunning) *cobra.Command {
	return &cobra.Command{
		Use:   "test <name>",
		Short: "Send one test message through a channel",
		Long: "test sends a short message through the channel once, without retries, and reports the outcome. A failure\n" +
			"carries the error class of the delivery, for example auth, unreachable, tls or timeout, as its exit code.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.NotifyTestResult
			if err := notifyCall(c, state, control.NotifyTestCommand, control.NotifyNameArgs{Name: args[0]}, &res); err != nil {
				return err
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
				{Name: "name", Value: res.Name}, {Name: "type", Value: res.Type}, {Name: "sent", Value: true},
			}})
		},
	}
}

// secretPrompts names the secret parts a channel needs, in the order they are read.
func secretPrompts(a control.NotifyAddArgs) []string {
	switch a.Type {
	case control.NotifyWebhook:
		return []string{"webhook URL", "signing key"}
	case control.NotifySlack:
		return []string{"Slack incoming-webhook URL"}
	case control.NotifyTelegram:
		return []string{"Telegram bot token"}
	case control.NotifySMTP:
		if a.User != "" {
			return []string{"SMTP password"}
		}
	}
	// An unknown type reads nothing; the service refuses it.
	return nil
}

// readSecrets reads one value per prompt: hidden at a terminal, otherwise one line each from standard input.
func readSecrets(c *cobra.Command, channel string, prompts []string) ([]string, error) {
	if len(prompts) == 0 {
		return nil, nil
	}
	in := stdinStream()
	values := make([]string, 0, len(prompts))
	if f, ok := in.(*os.File); ok && isTerminal(f) {
		for _, p := range prompts {
			_, _ = fmt.Fprintf(c.ErrOrStderr(), "%s for %s (input is hidden): ", p, channel)
			raw, err := readHidden(f)
			_, _ = fmt.Fprintln(c.ErrOrStderr())
			if err != nil {
				return nil, &cli.Failure{Code: output.CodeRuntime, Message: "cannot read the value from the terminal",
					NextStep: "Run the command again, or pipe the values on standard input.", Cause: err}
			}
			values = append(values, string(raw))
		}
		return values, nil
	}
	raw, err := io.ReadAll(io.LimitReader(in, int64(len(prompts))*(credentials.MaxValueBytes+3)))
	if err != nil {
		return nil, &cli.Failure{Code: output.CodeRuntime, Message: "cannot read the values from standard input",
			NextStep: "Run the command again.", Cause: err}
	}
	text := strings.TrimSuffix(string(raw), "\n")
	lines := strings.Split(text, "\n")
	if len(lines) != len(prompts) {
		return nil, usageFailure(fmt.Sprintf("standard input must hold %d line(s): %s", len(prompts), strings.Join(prompts, ", then ")),
			"Pipe the values one per line, or run the command at a terminal.")
	}
	for _, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		if l == "" {
			return nil, usageFailure("a secret value on standard input is empty", "Pipe the values one per line, or run the command at a terminal.")
		}
		values = append(values, l)
	}
	return values, nil
}

func notifyCall(c *cobra.Command, state *notRunning, command string, args, result any) error {
	if err := credentialCall(c, state, command, args, result); err != nil {
		var remote *control.Error
		var f *cli.Failure
		if errors.As(err, &f) && errors.As(f.Cause, &remote) {
			return notifyFailure(remote, f)
		}
		return err
	}
	return nil
}

// notifyFailure maps the codes of the channel commands; any other failure stays as the shared mapping made it.
func notifyFailure(remote *control.Error, shared *cli.Failure) error {
	switch remote.Code {
	case control.CodeChannelExists:
		return &cli.Failure{Code: output.CodeInvalidRequest, Message: remote.Message, Usage: true,
			NextStep: "Choose another name, or remove the channel first with 'qdeploy notify remove <name>'.", Cause: remote}
	case control.CodeNoSuchChannel:
		return &cli.Failure{Code: output.CodeNotFound, Message: remote.Message,
			NextStep: "List the channels with 'qdeploy notify list'.", Cause: remote}
	case string(output.CodeAuth), string(output.CodePermission), string(output.CodeNotFound), string(output.CodeRateLimited),
		string(output.CodeUnreachable), string(output.CodeTimeout), string(output.CodeTLS), string(output.CodeConfigInvalid),
		string(output.CodeInvalidRequest), string(output.CodeRuntime):
		return &cli.Failure{Code: output.Code(remote.Code), Message: remote.Message,
			NextStep: "Check the channel's settings and secrets (replace it with 'qdeploy notify remove' and 'qdeploy notify add'), then test it again.",
			Cause:    remote}
	}
	return shared
}

func channelSettings(ch control.NotifyChannelInfo) string {
	var parts []string
	add := func(name, value string) {
		if value != "" {
			parts = append(parts, name+"="+value)
		}
	}
	add("host", ch.Host)
	if ch.Port != 0 {
		add("port", fmt.Sprint(ch.Port))
	}
	add("security", ch.Security)
	add("user", ch.User)
	add("from", ch.From)
	add("to", strings.Join(ch.To, ","))
	add("chat_id", ch.ChatID)
	add("base_url", ch.BaseURL)
	return strings.Join(parts, " ")
}

func channelFields(ch control.NotifyChannelInfo) []output.Field {
	fields := []output.Field{{Name: "name", Value: ch.Name}, {Name: "type", Value: ch.Type},
		{Name: "events", Value: strings.Join(ch.Events, ",")}}
	if s := channelSettings(ch); s != "" {
		fields = append(fields, output.Field{Name: "settings", Value: s})
	}
	return fields
}
