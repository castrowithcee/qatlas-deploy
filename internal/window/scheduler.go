package window

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-kit/configfile"
)

const (
	fileName   = "window.json"
	fileSchema = 1
	// maxWait bounds one wait, so a wall clock that jumps is noticed within it.
	maxWait = time.Hour
	// retryWait is the wait after the setting could not be read or the run could not be recorded.
	retryWait = time.Minute
)

// Clock is the time source of the scheduler.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Job is the work of one opening. It runs in the scheduler's goroutine; open tells whether the window, as set
// now, is still open, and the job starts nothing new once it answers false. ctx ends when the service stops.
type Job func(ctx context.Context, open func() bool)

// Config is what New needs.
type Config struct {
	// StateDir is the service state directory; the setting is kept in a file there.
	StateDir string
	Job      Job
	// Clock is replaced by tests; nil means the system clock.
	Clock  Clock
	Logger *slog.Logger
}

// Change names the parts of the window to replace; a nil part keeps its value.
type Change struct {
	Days     *Days
	Start    *int
	Duration *time.Duration
	Zone     *string
}

// Status is the window as set, whether it is open and when it opens next. OpenUntil is set while it is open;
// Next is the opening after the current one then. LastRun is when the work of an opening last began.
type Status struct {
	Window    Window
	Open      bool
	OpenUntil time.Time
	Next      time.Time
	LastRun   time.Time
	Running   bool
}

// Scheduler runs the job once per opening of the window. The setting and the start of the last run are kept
// in the service state, so a restart within an opening whose work already began does not begin it again.
type Scheduler struct {
	cfg     Config
	store   *configfile.Store[record]
	changed chan struct{}

	mu      sync.Mutex
	running bool
}

// record is the stored setting with the start of the last run.
type record struct {
	Window  Window
	LastRun time.Time
}

type recordFile struct {
	Schema   int       `json:"schema"`
	Days     []string  `json:"days"`
	Start    string    `json:"start"`
	Duration string    `json:"duration"`
	Zone     string    `json:"zone"`
	LastRun  time.Time `json:"last_run,omitzero"`
}

// New prepares the scheduler; it creates no file before the first change.
func New(cfg Config) (*Scheduler, error) {
	if cfg.StateDir == "" || cfg.Job == nil {
		return nil, errors.New("window: state directory and job are required")
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	store, err := configfile.NewStore(filepath.Join(cfg.StateDir, fileName), configfile.Codec[record]{
		Validate: func(r record) error {
			if r.Window.check() != nil {
				return errMalformed
			}
			return nil
		},
		Encode: encodeRecord, Decode: decodeRecord,
	})
	if err != nil {
		return nil, err
	}
	return &Scheduler{cfg: cfg, store: store, changed: make(chan struct{}, 1)}, nil
}

func encodeRecord(r record) ([]byte, error) {
	return json.MarshalIndent(recordFile{
		Schema: fileSchema, Days: r.Window.Days.Names(), Start: FormatStart(r.Window.Start),
		Duration: FormatDuration(r.Window.Duration), Zone: r.Window.Zone, LastRun: r.LastRun.UTC(),
	}, "", "  ")
}

// errMalformed is a stored setting that does not decode; it is no *InvalidError, which answers a request.
var errMalformed = errors.New("the stored window setting is malformed")

func decodeRecord(b []byte) (record, error) {
	r, err := decodeFile(b)
	if err != nil {
		return record{}, errMalformed
	}
	return r, nil
}

func decodeFile(b []byte) (record, error) {
	var f recordFile
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return record{}, err
	}
	if f.Schema != fileSchema {
		return record{}, errors.New("unsupported schema")
	}
	days, err := ParseDays(f.Days)
	if err != nil {
		return record{}, err
	}
	start, err := ParseStart(f.Start)
	if err != nil {
		return record{}, err
	}
	d, err := ParseDuration(f.Duration)
	if err != nil {
		return record{}, err
	}
	if !validZoneName(f.Zone) {
		return record{}, invalid("invalid zone")
	}
	return record{Window: Window{Days: days, Start: start, Duration: d, Zone: f.Zone}, LastRun: f.LastRun}, nil
}

// load returns the stored record; a missing file is the default window that never ran.
func (s *Scheduler) load() (record, error) {
	r, _, err := s.store.LoadVersioned()
	if errors.Is(err, fs.ErrNotExist) {
		return record{Window: Default()}, nil
	}
	return r, err
}

func (s *Scheduler) update(fn func(record) (record, error)) (record, error) {
	var out record
	_, err := s.store.Update(func(r record) (record, error) {
		if r.Window == (Window{}) {
			r.Window = Default()
		}
		r, err := fn(r)
		out = r
		return r, err
	})
	return out, err
}

// Status returns the window as set and its openings as of now.
func (s *Scheduler) Status() (Status, error) {
	r, err := s.load()
	if err != nil {
		return Status{}, err
	}
	return s.status(r)
}

func (s *Scheduler) status(r record) (Status, error) {
	now := s.cfg.Clock.Now()
	st := Status{Window: r.Window, LastRun: r.LastRun}
	cur, open, err := r.Window.At(now)
	if err != nil {
		return Status{}, err
	}
	next, err := r.Window.Next(now)
	if err != nil {
		return Status{}, err
	}
	if open {
		st.Open, st.OpenUntil = true, cur.End
	}
	st.Next = next.Start
	if !st.LastRun.IsZero() {
		st.LastRun = st.LastRun.In(next.Start.Location())
	}
	s.mu.Lock()
	st.Running = s.running
	s.mu.Unlock()
	return st, nil
}

// Set replaces the named parts of the window and returns the new status. A zone is checked against the zone
// database. The scheduler picks the change up at once: a wait is computed again, and a running job sees the
// new end through open.
func (s *Scheduler) Set(c Change) (Status, error) {
	if c.Days == nil && c.Start == nil && c.Duration == nil && c.Zone == nil {
		return Status{}, invalid("name at least one part of the window to change")
	}
	if c.Zone != nil {
		if _, err := ParseZone(*c.Zone); err != nil {
			return Status{}, err
		}
	}
	r, err := s.update(func(r record) (record, error) {
		w := r.Window
		if c.Days != nil {
			w.Days = *c.Days
		}
		if c.Start != nil {
			w.Start = *c.Start
		}
		if c.Duration != nil {
			w.Duration = *c.Duration
		}
		if c.Zone != nil {
			w.Zone = *c.Zone
		}
		if err := w.check(); err != nil {
			return r, err
		}
		r.Window = w
		return r, nil
	})
	if err != nil {
		return Status{}, err
	}
	select {
	case s.changed <- struct{}{}:
	default:
	}
	return s.status(r)
}

// open tells whether the window as stored now is open. A setting that cannot be read counts as closed.
func (s *Scheduler) open() bool {
	r, err := s.load()
	if err != nil {
		return false
	}
	_, ok, err := r.Window.At(s.cfg.Clock.Now())
	return err == nil && ok
}

// Run waits for the openings and runs the job in each until ctx ends. The job runs at most once per opening:
// when the window is open and no run began since the opening started. The start of a run is stored before the
// job begins, so a run cut short by a stop is not repeated in the same opening.
func (s *Scheduler) Run(ctx context.Context) {
	for ctx.Err() == nil {
		wait := s.cycle(ctx)
		if wait <= 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-s.changed:
		case <-s.cfg.Clock.After(min(wait, maxWait)):
		}
	}
}

// cycle runs the job if it is due and returns how long to wait before looking again.
func (s *Scheduler) cycle(ctx context.Context) time.Duration {
	log := s.cfg.Logger
	r, err := s.load()
	if err != nil {
		log.Error("the maintenance window setting cannot be read; no window runs until it is fixed", "error", err)
		return retryWait
	}
	now := s.cfg.Clock.Now()
	cur, open, err := r.Window.At(now)
	if err != nil {
		log.Error("the maintenance window cannot be computed", "error", err)
		return retryWait
	}
	if open && r.LastRun.Before(cur.Start) {
		// Running is set first, so no status shows the new start without it.
		s.setRunning(true)
		defer s.setRunning(false)
		if _, err := s.update(func(r record) (record, error) {
			r.LastRun = now.UTC()
			return r, nil
		}); err != nil {
			log.Error("the start of the maintenance window run cannot be recorded; it is not run", "error", err)
			return retryWait
		}
		log.Info("maintenance window opened", "until", cur.End.Format(time.RFC3339))
		s.cfg.Job(ctx, s.open)
		log.Info("maintenance window run finished")
		return 0
	}
	next, err := r.Window.Next(now)
	if err != nil {
		log.Error("the maintenance window cannot be computed", "error", err)
		return retryWait
	}
	return next.Start.Sub(now)
}

func (s *Scheduler) setRunning(on bool) {
	s.mu.Lock()
	s.running = on
	s.mu.Unlock()
}
