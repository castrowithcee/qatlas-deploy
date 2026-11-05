package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

const settingsFields = "The settings are one object with these members, each optional except version:\n\n" +
	"  version: 1\n" +
	"  compose.files: the compose files in merge order, relative to the stack folder\n" +
	"  update.mode: automatic or manual (manual); update.ceiling: patch, minor or major (minor);\n" +
	"  update.major: direct or stepwise (stepwise); update.tags: image version variable to a regular\n" +
	"  expression its tags must match; update.changelog: image version variable to a GitHub repository\n" +
	"  backup.paths, backup.volumes, backup.command: data directories below the stack folder, named volumes\n" +
	"  and an argument vector; backup.retention.keep and backup.retention.max_age_days (keep 7)\n" +
	"  check.containers: true or false (true); check.http.url, check.http.status (200), check.http.timeout (10s)\n\n" +
	"A stack without settings is deployed but never updated."

func newSettingsCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	show := &cobra.Command{
		Use:   "settings <stack>",
		Short: "Show or replace the settings of a stack: update, backup, check and compose files",
		Long: "settings shows the settings of the stack with every default written out. The service keeps them in\n" +
			"the stacks repository and changes them only on request, from the TUI or with 'qdeploy stack settings\n" +
			"set'; they are not edited by hand. --output json gives the object 'qdeploy stack settings set' takes as\n" +
			"its settings member.\n\n" + settingsFields,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageFailure("settings takes exactly one stack", "Run 'qdeploy stack settings <stack>'; 'qdeploy stack list' names the stacks.")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			var res control.StackSettingsResult
			if err := credentialCall(c, state, control.StackSettingsGetCommand, control.StackSettingsArgs{Stack: args[0]}, &res); err != nil {
				return err
			}
			fields := []output.Field{{Name: "stack", Value: res.Stack}, {Name: "configured", Value: res.Settings != nil}}
			if res.Settings != nil {
				fields = append(fields, settingsOutput(program.Format(), *res.Settings)...)
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: fields})
		},
	}
	var file string
	set := &cobra.Command{
		Use:   "set <stack> --file <path>",
		Short: "Replace the settings of a stack",
		Long: "set replaces all settings of the stack with the object read from --file, as YAML or JSON; '-' reads\n" +
			"standard input. A member left out takes its default, so give the whole object, for example the\n" +
			"settings member of 'qdeploy stack settings <stack> --output json' with the changes made. The service\n" +
			"checks the settings against the stack, commits them to the stacks repository, pushes the commit and\n" +
			"moves the working copy to it; the deploy key needs write access to the branch. It waits until no run\n" +
			"is active. Settings that do not fit change nothing, and settings equal to the present ones commit\n" +
			"nothing (changed false).\n\n" + settingsFields,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageFailure("settings set takes exactly one stack", "Run 'qdeploy stack settings set <stack> --file <path>'.")
			}
			if file == "" {
				return usageFailure("settings set needs --file", "Run 'qdeploy stack settings set <stack> --file <path>', or --file - to read standard input.")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			s, err := readSettingsFile(file)
			if err != nil {
				return err
			}
			var res control.StackSettingsSetResult
			if err := remoteCall(c, state, control.StackSettingsTimeout, control.StackSettingsSetCommand,
				control.StackSettingsSetArgs{Stack: args[0], Settings: s}, &res); err != nil {
				return err
			}
			fields := []output.Field{{Name: "stack", Value: res.Stack}, {Name: "changed", Value: res.Changed}}
			if res.Commit != "" {
				fields = append(fields, output.Field{Name: "commit", Value: res.Commit})
			}
			fields = append(fields, settingsOutput(program.Format(), res.Settings)...)
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: fields})
		},
	}
	set.Flags().StringVar(&file, "file", "", "the settings as YAML or JSON; - reads standard input")
	show.AddCommand(set)
	return show
}

// readSettingsFile reads the settings object from path, or from standard input for "-". The file is YAML or
// JSON; an unknown member, a repeated key or a second document is a usage error. The service checks the
// values.
func readSettingsFile(path string) (control.StackSettings, error) {
	var in io.Reader
	if path == "-" {
		in = stdinStream()
	} else {
		f, err := os.Open(path)
		if err != nil {
			return control.StackSettings{}, &cli.Failure{Code: output.CodeNotFound, Message: "cannot read the settings file",
				NextStep: "Check the path given with --file.", Cause: err}
		}
		defer f.Close()
		in = f
	}
	data, err := io.ReadAll(io.LimitReader(in, stacks.MaxFileSize+1))
	if err != nil {
		return control.StackSettings{}, &cli.Failure{Code: output.CodeRuntime, Message: "cannot read the settings",
			NextStep: "Check the file given with --file and run the command again.", Cause: err}
	}
	invalid := func(why string) error {
		return usageFailure("the settings are not valid: "+why,
			"Give one object as YAML or JSON with the members 'qdeploy stack settings --help' lists.")
	}
	if len(data) > stacks.MaxFileSize {
		return control.StackSettings{}, invalid(fmt.Sprintf("larger than %d bytes", stacks.MaxFileSize))
	}
	var doc any
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return control.StackSettings{}, invalid("the input is empty")
		}
		return control.StackSettings{}, invalid(strings.TrimPrefix(err.Error(), "yaml: "))
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return control.StackSettings{}, invalid("only one document is allowed")
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return control.StackSettings{}, invalid(err.Error())
	}
	var s control.StackSettings
	jd := json.NewDecoder(bytes.NewReader(raw))
	jd.DisallowUnknownFields()
	if err := jd.Decode(&s); err != nil {
		return control.StackSettings{}, invalid(strings.TrimPrefix(err.Error(), "json: "))
	}
	return s, nil
}

// settingsOutput lists the settings as one structured field for JSON and TOON, and as one scalar field per
// setting, named by its path, for the text formats.
func settingsOutput(format output.Format, s control.StackSettings) []output.Field {
	if format == output.FormatJSON || format == output.FormatTOON {
		return []output.Field{{Name: "settings", Value: structured(format, s)}}
	}
	fields := []output.Field{{Name: "version", Value: s.Version}}
	add := func(name string, v any) { fields = append(fields, output.Field{Name: name, Value: v}) }
	list := func(v []string) string { return strings.Join(v, ",") }
	byKey := func(prefix string, m map[string]string) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			add(prefix+"."+k, m[k])
		}
	}
	if c := s.Compose; c != nil {
		add("compose.files", list(c.Files))
	}
	if u := s.Update; u != nil {
		add("update.mode", u.Mode)
		add("update.ceiling", u.Ceiling)
		add("update.major", u.Major)
		byKey("update.tags", u.Tags)
		byKey("update.changelog", u.Changelog)
	}
	if b := s.Backup; b != nil {
		add("backup.paths", list(b.Paths))
		add("backup.volumes", list(b.Volumes))
		if len(b.Command) > 0 {
			cmd, _ := json.Marshal(b.Command)
			add("backup.command", string(cmd))
		}
		if r := b.Retention; r != nil {
			if r.Keep > 0 {
				add("backup.retention.keep", r.Keep)
			}
			if r.MaxAgeDays > 0 {
				add("backup.retention.max_age_days", r.MaxAgeDays)
			}
		}
	}
	if ch := s.Check; ch != nil {
		if ch.Containers != nil {
			add("check.containers", *ch.Containers)
		}
		if h := ch.HTTP; h != nil {
			add("check.http.url", h.URL)
			add("check.http.status", h.Status)
			add("check.http.timeout", h.Timeout)
		}
	}
	return fields
}
