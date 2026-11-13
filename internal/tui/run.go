package tui

import (
	"context"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

// Options is what Run needs to start the interface on a terminal.
type Options struct {
	Backend Backend
	// Folders prepares a stack folder on the host before the service clones into it; nil skips the step.
	Folders Folders
	In, Out *os.File
	// Prefs are the device-local preferences; PrefsWarnings the problems found while loading them, of which the
	// first is shown at start.
	Prefs         uiprefs.Prefs
	PrefsWarnings []string
	// SavePrefs writes the preferences to the device; nil when this device has no place for them.
	SavePrefs func(uiprefs.Prefs) error
}

// PrefsSchema is what the preferences file of the interface may hold.
func PrefsSchema() (uiprefs.Schema, error) {
	themes, err := kittui.NewThemeSet(nil)
	if err != nil {
		return uiprefs.Schema{}, err
	}
	var ids []string
	for _, a := range areas() {
		ids = append(ids, string(a.ID))
	}
	return uiprefs.Schema{Themes: themes.Names(), Areas: ids, DefaultArea: string(areaStacks)}, nil
}

// Run shows the interface on the terminal of opts until the person quits.
func Run(opts Options) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env := os.Environ()
	profile := kittui.DetectColorProfile(opts.Out, env)
	m, err := newModel(config{ctx: ctx, backend: opts.Backend, folders: opts.Folders, lang: uiprefs.Language(opts.Prefs.Language),
		theme: opts.Prefs.Theme, profile: profile, charset: kittui.DetectCharsetMode(env),
		start: kittui.AreaID(opts.Prefs.Start), loc: time.Local, warnings: opts.PrefsWarnings, mouse: opts.Prefs.MouseEnabled,
		prefs: opts.Prefs, savePrefs: opts.SavePrefs})
	if err != nil {
		return err
	}
	input := kittui.NewInputFilter(nil)
	program := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(opts.In), tea.WithOutput(opts.Out),
		tea.WithFilter(input.Filter), tea.WithColorProfile(colorProfile(profile)))
	input.Attach(program.Send)
	_, err = program.Run()
	return err
}

func colorProfile(p kittui.ColorProfile) colorprofile.Profile {
	switch p {
	case kittui.ProfileTrueColor:
		return colorprofile.TrueColor
	case kittui.ProfileANSI256:
		return colorprofile.ANSI256
	case kittui.ProfileANSI16:
		return colorprofile.ANSI
	case kittui.ProfileMono:
		return colorprofile.ASCII
	}
	return colorprofile.NoTTY
}
