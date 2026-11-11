package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
)

// stRdonly is ST_RDONLY, the statfs flag of a file system mounted read-only.
const stRdonly = 1

const readOnlyReason = "the folder lies on a read-only file system"

func newSourceCommand(program *cobracli.Program, state *notRunning) *cobra.Command {
	group := &cobra.Command{
		Use:   "source",
		Short: "Look at a folder before it is taken over as a stack folder",
		Long: "The source commands work on the stack folders of the host. Only root may use the service channel, so\n" +
			"run them with sudo. See 'qdeploy help sources'.",
	}
	var url string
	inspect := &cobra.Command{
		Use:   "inspect <path>",
		Short: "Classify a folder and show its permissions, files, stacks and container conflicts; changes nothing",
		Long: "inspect tells which case applies to the folder: clone (missing or empty, gets a clone of the URL),\n" +
			"adopt-clone (a clone with an origin remote), local (a repository without a remote), files (files\n" +
			"without a repository; the first commit would take the listed files and leave out the listed ones with\n" +
			"the reason) or reject (with the reason and what to do). It shows the owner, group and mode of the\n" +
			"folder, whether the service can write there and whether source add would have to set permissions\n" +
			"(group qdeploy, group write, setgid), and per stack the functions, the findings and the containers\n" +
			"of other Compose projects that run in the stack's folder: deploying would create duplicates of them.\n" +
			"--url is the SSH address to clone or to expect as origin. A folder on a read-only file system is\n" +
			"rejected. A rejection is a complete answer and exits 0; read the case. Nothing is created, changed\n" +
			"or fetched, and no value of an env file is shown. Run it with sudo.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageFailure("source inspect takes exactly one argument, the path of the folder",
					"Run 'qdeploy source inspect <path> [--url <ssh-url>]'.")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			return runSourceInspect(c, program, state, args[0], url)
		},
	}
	inspect.Flags().StringVar(&url, "url", "", "SSH address of the repository to clone, or to expect as origin")
	group.AddCommand(inspect, newSourceAddCommand(program, state))
	return group
}

func runSourceInspect(c *cobra.Command, program *cobracli.Program, state *notRunning, path, url string) error {
	res, err := inspectSource(c, state, path, url)
	if err != nil {
		return err
	}
	if !structuredFormat(program.Format()) && res.Access != nil && res.Access.GitWritable != nil && !*res.Access.GitWritable {
		_, _ = fmt.Fprintln(c.ErrOrStderr(), gitNotWritableWarning)
	}
	return output.Encode(c.OutOrStdout(), program.Format(), sourceResult(program.Format(), res))
}

const gitNotWritableWarning = "warning: the service cannot write .git; source add sets the group permissions, " +
	"or commit as a member of the group qdeploy or with sudo -u qdeploy git"

func structuredFormat(f output.Format) bool { return f == output.FormatJSON || f == output.FormatTOON }

// inspectSource validates the arguments, asks the service and adds what only this client can tell.
func inspectSource(c *cobra.Command, state *notRunning, path, url string) (control.SourceInspectResult, error) {
	var res control.SourceInspectResult
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return res, usageFailure("the path must be absolute and clean", "Pass the absolute path of the folder.")
	}
	if url != "" {
		if err := gitrepo.ValidateURL(url); err != nil {
			return res, usageFailure(err.Error(), "Pass the SSH address of the repository.")
		}
	}
	if err := credentialCall(c, state, control.SourceInspectCommand, control.SourceInspectArgs{Path: path, URL: url}, &res); err != nil {
		return res, err
	}
	// The service is sandboxed and sees every path that is not granted as read-only, so only this client can tell.
	if res.Case != control.SourceCaseReject {
		readOnly, err := folders.readOnly(path)
		if err != nil {
			return res, folderFailure("cannot inspect the file system of "+path, err)
		}
		if readOnly {
			res = control.SourceInspectResult{Path: res.Path, Case: control.SourceCaseReject, Reason: readOnlyReason,
				Fix:    "Choose a folder on a writable file system, or remount the file system read-write.",
				Stacks: []control.SourceStack{}}
		}
	}
	return res, nil
}

// pathOnReadOnlyFS reports whether the file system of path, or of its nearest existing parent while it is
// missing, is mounted read-only.
func pathOnReadOnlyFS(path string) (bool, error) {
	target := path
	for {
		_, err := os.Lstat(target)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return false, err
		}
		parent := filepath.Dir(target)
		if parent == target {
			break
		}
		target = parent
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(target, &st); err != nil {
		return false, err
	}
	return st.Flags&stRdonly != 0, nil
}

// sourceResult is the nested result for JSON and TOON and one flat field list for the text formats.
func sourceResult(format output.Format, res control.SourceInspectResult) output.Object {
	if structuredFormat(format) {
		fields := []output.Field{{Name: "path", Value: res.Path}, {Name: "case", Value: res.Case}}
		// An empty string would stay in the output, so a field without a value is left out here.
		add := func(name string, value any) { fields = append(fields, output.Field{Name: name, Value: value}) }
		addText := func(name, value string) {
			if value != "" {
				add(name, value)
			}
		}
		addText("url", res.URL)
		if res.NeedsURL {
			add("needs_url", true)
		}
		addText("reason", res.Reason)
		addText("fix", res.Fix)
		add("access", structured(format, res.Access))
		add("planned_files", res.PlannedFiles)
		add("excluded", structured(format, res.Excluded))
		add("stacks", structured(format, res.Stacks))
		addText("stacks_error", res.StacksError)
		add("containers_checked", res.ContainersChecked)
		addText("containers_error", res.ContainersError)
		return output.Object{Fields: fields}
	}
	f := []output.Field{{Name: "path", Value: res.Path}, {Name: "case", Value: res.Case}}
	add := func(name string, value any) { f = append(f, output.Field{Name: name, Value: value}) }
	if res.Reason != "" {
		add("reason", res.Reason)
		add("fix", res.Fix)
	}
	switch {
	case res.NeedsURL:
		add("url", "none; pass --url <ssh-url> to clone")
	case res.URL != "":
		add("url", res.URL)
	}
	if a := res.Access; a != nil {
		add("owner", nameOrID(a.Owner, a.OwnerID))
		add("group", nameOrID(a.Group, a.GroupID))
		add("mode", a.Mode)
		if a.ModePath != res.Path {
			add("mode_path", a.ModePath)
		}
		add("service_can_write", a.ServiceCanWrite)
		add("source_add_sets_permissions", a.SetsPermissions)
		if a.GitWritable != nil {
			add("git_writable", *a.GitWritable)
		}
	}
	for _, p := range res.PlannedFiles {
		add("planned_file", p)
	}
	for _, e := range res.Excluded {
		value := e.Path + ": " + e.Reason
		if e.Stack != "" {
			value += " (stack " + e.Stack + ")"
		}
		add("excluded", value)
	}
	for _, st := range res.Stacks {
		p := "stack." + st.Name + "."
		add(p+"folder", st.Folder)
		for _, fn := range st.Functions {
			value := "no"
			if fn.Works {
				value = "yes"
			}
			if fn.Mode != "" {
				value += ", " + fn.Mode
			}
			value += ": " + fn.Reason
			if fn.Action != "" {
				value += " Action: " + fn.Action
			}
			add(p+fn.Function, value)
		}
		for _, fd := range st.Findings {
			add(p+"finding."+fd.Rule, fd.Severity+": "+fd.Cause+" Fix: "+fd.Fix)
		}
		for _, cf := range st.Conflicts {
			add(p+"conflict", fmt.Sprintf("container %s of project %s runs in this folder; deploying would create duplicate containers",
				cf.Container, cf.Project))
		}
	}
	if res.StacksError != "" {
		add("stacks_error", res.StacksError)
	}
	if res.ContainersError != "" {
		add("containers_error", res.ContainersError)
	}
	return output.Object{Fields: f}
}

func nameOrID(name string, id uint32) string {
	if name != "" {
		return name
	}
	return strconv.FormatUint(uint64(id), 10)
}
