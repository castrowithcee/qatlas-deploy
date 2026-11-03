package notifier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	kitnotify "github.com/castrowithcee/qatlas-kit/notify"
)

// Sender delivers one message over one channel; *notify.Sender of the kit is one.
type Sender interface {
	Send(ctx context.Context, ch kitnotify.Channel, m kitnotify.Message) error
}

// Defaults of a Dispatcher.
const (
	DefaultQueue   = 64
	DefaultTimeout = 3 * time.Minute
	DefaultGrace   = 5 * time.Second
)

// DispatchConfig is what NewDispatcher needs.
type DispatchConfig struct {
	Sender Sender
	// Channels returns the channels as set now.
	Channels func() ([]Channel, error)
	Creds    CredentialReader
	Log      *slog.Logger
	// Queue is how many events may wait; zero means DefaultQueue. A full queue drops the event.
	Queue int
	// Timeout bounds the delivery of one event to all its channels; zero means DefaultTimeout.
	Timeout time.Duration
	// Grace is how long Close lets the queue drain before it cancels the deliveries; zero means DefaultGrace.
	Grace time.Duration
	// RunLog, if set, is called from the delivery goroutine with the run of an event and a line about a
	// failed delivery. Events without a run do not call it; its failure is its own business.
	RunLog func(run, text string)
}

type job struct {
	ev       Event
	channels []Channel
}

// Dispatcher delivers events in its own goroutine, so a slow or broken channel never holds the caller. A
// failed delivery is logged and never retried beyond the kit's own retries.
type Dispatcher struct {
	cfg    DispatchConfig
	queue  chan job
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	closed bool
}

// NewDispatcher starts the delivery goroutine; Close stops it.
func NewDispatcher(cfg DispatchConfig) *Dispatcher {
	if cfg.Queue <= 0 {
		cfg.Queue = DefaultQueue
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Grace <= 0 {
		cfg.Grace = DefaultGrace
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	d := &Dispatcher{cfg: cfg, queue: make(chan job, cfg.Queue), done: make(chan struct{})}
	d.ctx, d.cancel = context.WithCancel(context.Background())
	go d.work()
	return d
}

// Submit queues the event for the channels that selected it and returns at once. It tells whether the event
// was queued: false when no channel selected it, the settings cannot be read, the queue is full or the
// dispatcher is closed.
func (d *Dispatcher) Submit(ev Event) bool {
	list, err := d.cfg.Channels()
	if err != nil {
		d.cfg.Log.Warn("notification not sent: the channel settings cannot be read", "event", ev.Type, "stack", ev.Stack)
		return false
	}
	var to []Channel
	for _, c := range list {
		if c.Subscribes(ev.Type) {
			to = append(to, c)
		}
	}
	if len(to) == 0 {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return false
	}
	select {
	case d.queue <- job{ev: ev, channels: to}:
		return true
	default:
		d.cfg.Log.Warn("notification dropped: the queue is full", "event", ev.Type, "stack", ev.Stack, "run", ev.Run)
		return false
	}
}

// Close stops taking events, gives the queued ones the grace time to leave and then cancels what is left.
func (d *Dispatcher) Close() {
	d.mu.Lock()
	if !d.closed {
		d.closed = true
		close(d.queue)
	}
	d.mu.Unlock()
	t := time.NewTimer(d.cfg.Grace)
	defer t.Stop()
	select {
	case <-d.done:
	case <-t.C:
		d.cancel()
		<-d.done
	}
	d.cancel()
}

func (d *Dispatcher) work() {
	defer close(d.done)
	for j := range d.queue {
		d.deliver(j)
	}
}

// deliver sends one event to its channels side by side, so one that hangs does not delay the others.
func (d *Dispatcher) deliver(j job) {
	ctx, cancel := context.WithTimeout(d.ctx, d.cfg.Timeout)
	defer cancel()
	msg := Message(j.ev)
	var wg sync.WaitGroup
	for _, c := range j.channels {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.send(ctx, c, j.ev, msg)
		}()
	}
	wg.Wait()
}

func (d *Dispatcher) send(ctx context.Context, c Channel, ev Event, msg kitnotify.Message) {
	attrs := []any{"channel", c.Name, "event", ev.Type, "stack", ev.Stack, "run", ev.Run}
	defer func() {
		if p := recover(); p != nil {
			d.cfg.Log.Error("notification not delivered: the sender panicked", append(attrs, "panic", fmt.Sprint(p))...)
		}
	}()
	err := Send(ctx, d.cfg.Sender, d.cfg.Creds, c, msg)
	if err != nil {
		d.cfg.Log.Warn("notification not delivered", append(attrs, "code", ErrorCode(err), "error", err.Error())...)
		if ev.Run != "" && d.cfg.RunLog != nil {
			d.cfg.RunLog(ev.Run, fmt.Sprintf("notification to channel %s not delivered: %s: %s", c.Name, ErrorCode(err), err.Error()))
		}
		return
	}
	d.cfg.Log.Info("notification delivered", attrs...)
}

// Send delivers a message over a channel, reading its credentials first. A failure of the kit is an
// *kitnotify.Error, already redacted.
func Send(ctx context.Context, s Sender, creds CredentialReader, c Channel, m kitnotify.Message) error {
	ch, err := Kit(c, creds)
	if err != nil {
		return err
	}
	return s.Send(ctx, ch, m)
}

// ErrorCode is the code of a failure of the kit; "runtime" for any other error.
func ErrorCode(err error) string {
	var ke *kitnotify.Error
	if errors.As(err, &ke) {
		return string(ke.Code)
	}
	return "runtime"
}
