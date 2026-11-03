package notifier

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-kit/configfile"
)

const (
	settingsFile = "notify.json"
	seenFile     = "notify-updates.json"
	fileSchema   = 1
	maxSeen      = 4096
)

// The failures a caller tells apart.
var (
	ErrExists   = errors.New("a channel of this name exists")
	ErrNotFound = errors.New("no such channel")
)

type settingsDoc struct {
	Schema   int       `json:"schema"`
	Channels []Channel `json:"channels"`
}

// Settings is the file of the non-secret channel settings in the state directory. The file is private to the
// service (mode 0600) and holds no secret.
type Settings struct {
	store *configfile.Store[settingsDoc]
}

func strictDecode(b []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(into)
}

// OpenSettings prepares the settings file in stateDir; nothing is written before the first change.
func OpenSettings(stateDir string) (*Settings, error) {
	st, err := configfile.NewStore(filepath.Join(stateDir, settingsFile), configfile.Codec[settingsDoc]{
		Validate: func(d settingsDoc) error {
			if d.Schema != fileSchema || len(d.Channels) > MaxChannels {
				return errors.New("unsupported settings")
			}
			seen := map[string]bool{}
			for _, c := range d.Channels {
				if seen[c.Name] {
					return errors.New("channel named twice")
				}
				seen[c.Name] = true
				if _, err := Validate(c, nil); err != nil {
					return errors.New("invalid channel")
				}
			}
			return nil
		},
		Encode: func(d settingsDoc) ([]byte, error) { return json.MarshalIndent(d, "", "  ") },
		Decode: func(b []byte) (settingsDoc, error) {
			var d settingsDoc
			return d, strictDecode(b, &d)
		},
	})
	if err != nil {
		return nil, err
	}
	return &Settings{store: st}, nil
}

// List returns the channels in name order; no file means no channels.
func (s *Settings) List() ([]Channel, error) {
	d, _, err := s.store.LoadVersioned()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d.Channels, nil
}

// Get returns one channel, ErrNotFound when it does not exist.
func (s *Settings) Get(name string) (Channel, error) {
	list, err := s.List()
	if err != nil {
		return Channel{}, err
	}
	for _, c := range list {
		if c.Name == name {
			return c, nil
		}
	}
	return Channel{}, ErrNotFound
}

// Add stores a validated channel; ErrExists when the name is taken.
func (s *Settings) Add(c Channel) error {
	_, err := s.store.Update(func(d settingsDoc) (settingsDoc, error) {
		d.Schema = fileSchema
		if slices.ContainsFunc(d.Channels, func(x Channel) bool { return x.Name == c.Name }) {
			return d, ErrExists
		}
		if len(d.Channels) >= MaxChannels {
			return d, invalid("the host has the most channels it may have")
		}
		d.Channels = append(d.Channels, c)
		slices.SortFunc(d.Channels, func(a, b Channel) int { return strings.Compare(a.Name, b.Name) })
		return d, nil
	})
	return err
}

// Remove deletes a channel; ErrNotFound when it does not exist.
func (s *Settings) Remove(name string) error {
	_, err := s.store.Update(func(d settingsDoc) (settingsDoc, error) {
		i := slices.IndexFunc(d.Channels, func(x Channel) bool { return x.Name == name })
		if i < 0 {
			return d, ErrNotFound
		}
		d.Schema = fileSchema
		d.Channels = slices.Delete(d.Channels, i, i+1)
		return d, nil
	})
	return err
}

type seenDoc struct {
	Schema int `json:"schema"`
	// Versions maps stack, then variable, to the version an update notice was last made for.
	Versions map[string]map[string]string `json:"versions"`
}

// Seen records for which version of which variable of which stack an update notice was made, so a restart
// or the next opening of the maintenance window does not repeat it.
type Seen struct {
	store *configfile.Store[seenDoc]
}

// OpenSeen prepares the record in stateDir.
func OpenSeen(stateDir string) (*Seen, error) {
	st, err := configfile.NewStore(filepath.Join(stateDir, seenFile), configfile.Codec[seenDoc]{
		Validate: func(d seenDoc) error {
			if d.Schema != fileSchema {
				return errors.New("unsupported record")
			}
			return nil
		},
		Encode: func(d seenDoc) ([]byte, error) { return json.MarshalIndent(d, "", "  ") },
		Decode: func(b []byte) (seenDoc, error) {
			var d seenDoc
			return d, strictDecode(b, &d)
		},
	})
	if err != nil {
		return nil, err
	}
	return &Seen{store: st}, nil
}

// Fresh returns the items whose version no notice was made for yet. An unreadable record is an error, so the
// caller does not repeat every notice.
func (s *Seen) Fresh(stack string, items []Item) ([]Item, error) {
	d, _, err := s.store.LoadVersioned()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read the update notices: %w", err)
	}
	var out []Item
	for _, it := range items {
		if d.Versions[stack][it.Variable] != it.Available {
			out = append(out, it)
		}
	}
	return out, nil
}

// Mark records that notices were made for the items.
func (s *Seen) Mark(stack string, items []Item) error {
	_, err := s.store.Update(func(d seenDoc) (seenDoc, error) {
		d.Schema = fileSchema
		if d.Versions == nil {
			d.Versions = map[string]map[string]string{}
		}
		if d.Versions[stack] == nil {
			if len(d.Versions) >= maxSeen {
				return d, errors.New("too many stacks in the update record")
			}
			d.Versions[stack] = map[string]string{}
		}
		for _, it := range items {
			d.Versions[stack][it.Variable] = it.Available
		}
		return d, nil
	})
	return err
}
