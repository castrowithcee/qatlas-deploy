package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/castrowithcee/qatlas-kit/notify"
	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-deploy/internal/control"
	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
	"github.com/castrowithcee/qatlas-deploy/internal/engine"
	"github.com/castrowithcee/qatlas-deploy/internal/notifier"
)

var (
	errNotifyUnreadable = control.NewError(control.CodeInternal,
		"the notification settings cannot be read or stored; see the service log")
	errNotifyCredentials = control.NewError(control.CodeInternal,
		"the credentials of the channel cannot be read or stored; see the service log")
)

// notifyService owns the notification channels: their settings and credentials, the commands that manage them
// and the dispatcher that delivers run events and update notices.
type notifyService struct {
	settings *notifier.Settings
	seen     *notifier.Seen
	creds    *credentials.Store
	disp     *notifier.Dispatcher
	test     notifier.Sender
	secrets  *redact.Redactor
	log      *slog.Logger
	mu       sync.Mutex // serializes the commands that change channels
	// appendLog is set once the engine exists, which is created after this service.
	appendLog atomic.Pointer[func(run, text string) error]
}

func newNotifyService(stateDir string, creds *credentials.Store, opts notify.Options, secrets *redact.Redactor,
	version string, log *slog.Logger) (*notifyService, error) {
	settings, err := notifier.OpenSettings(stateDir)
	if err != nil {
		return nil, err
	}
	seen, err := notifier.OpenSeen(stateDir)
	if err != nil {
		return nil, err
	}
	opts.Redactor = secrets
	if opts.UserAgent == "" {
		opts.UserAgent = "qdeploy/" + version
	}
	sender, err := notify.New(opts)
	if err != nil {
		return nil, err
	}
	// A test reports the first failure at once instead of retrying for minutes.
	opts.Attempts = 1
	test, err := notify.New(opts)
	if err != nil {
		return nil, err
	}
	n := &notifyService{settings: settings, seen: seen, creds: creds, test: test, secrets: secrets, log: log}
	n.disp = notifier.NewDispatcher(notifier.DispatchConfig{
		Sender: sender, Channels: settings.List, Creds: creds, Log: log, RunLog: n.runLog,
	})
	return n, nil
}

// bindRunLog connects the dispatcher to the engine's run logs.
func (n *notifyService) bindRunLog(f func(run, text string) error) { n.appendLog.Store(&f) }

// runLog records a failed delivery in the log of its run, which may be gone by now; the service log keeps
// the failure either way. The text is redacted again, on top of the kit's own redaction.
func (n *notifyService) runLog(run, text string) {
	f := n.appendLog.Load()
	if f == nil {
		return
	}
	if err := (*f)(run, n.secrets.Apply(text)); err != nil && !errors.Is(err, engine.ErrNoSuchRun) {
		n.log.Warn("notification failure not recorded in the run log", "run", run, "error", err)
	}
}

// close drains the dispatcher; the service calls it after the engine has stopped.
func (n *notifyService) close() { n.disp.Close() }

// runEvent is the engine's event hook. It only queues, so it returns at once whatever the channels do.
func (n *notifyService) runEvent(ev engine.Event) {
	n.disp.Submit(notifier.Event{Type: ev.Kind, Stack: ev.Stack, Run: ev.Run, Commit: ev.Commit, Error: ev.Error})
}

// updateAvailable queues an update notice for the versions no notice was made for yet. The window reports
// only stacks in manual mode. A version is recorded once it is queued; while no channel selects the event it
// stays unrecorded, so a channel added later hears of the update at the next opening.
func (n *notifyService) updateAvailable(notice UpdateNotice) {
	var items []notifier.Item
	for _, it := range notice.Items {
		if it.Status == engine.UpdateAvailable && it.Selected != "" {
			items = append(items, notifier.Item{Variable: it.Variable, Current: it.Current, Available: it.Selected})
		}
	}
	fresh, err := n.seen.Fresh(notice.Stack, items)
	if err != nil {
		n.log.Warn("update notice skipped: the record of sent notices cannot be read", "stack", notice.Stack)
		return
	}
	if len(fresh) == 0 {
		return
	}
	if !n.disp.Submit(notifier.Event{Type: notifier.EventUpdateAvailable, Stack: notice.Stack, Items: fresh}) {
		return
	}
	if err := n.seen.Mark(notice.Stack, fresh); err != nil {
		n.log.Warn("the record of sent update notices cannot be stored", "stack", notice.Stack)
	}
}

func registerNotifyCommands(r *control.Registry, n *notifyService) error {
	for name, h := range map[string]control.Handler{
		control.NotifyListCommand:   n.handleList,
		control.NotifyAddCommand:    n.handleAdd,
		control.NotifyRemoveCommand: n.handleRemove,
		control.NotifyTestCommand:   n.handleTest,
	} {
		if err := r.Register(name, h); err != nil {
			return err
		}
	}
	return nil
}

func channelInfo(c notifier.Channel) control.NotifyChannelInfo {
	return control.NotifyChannelInfo{
		Name: c.Name, Type: c.Type, Events: c.Events, Host: c.Host, Port: c.Port, Security: c.Security,
		User: c.User, From: c.From, To: c.To, ChatID: c.ChatID, BaseURL: c.BaseURL,
	}
}

func (n *notifyService) handleList(_ context.Context, raw json.RawMessage) (any, error) {
	if err := decodeArgs(raw, &struct{}{}); err != nil {
		return nil, err
	}
	list, err := n.settings.List()
	if err != nil {
		n.log.Error("notification settings not readable", "error", err)
		return nil, errNotifyUnreadable
	}
	res := control.NotifyListResult{Channels: make([]control.NotifyChannelInfo, 0, len(list))}
	for _, c := range list {
		res.Channels = append(res.Channels, channelInfo(c))
	}
	return res, nil
}

func (n *notifyService) handleAdd(_ context.Context, raw json.RawMessage) (any, error) {
	var a control.NotifyAddArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	c, err := notifier.Validate(notifier.Channel{
		Name: a.Name, Type: a.Type, Events: a.Events, Host: a.Host, Port: a.Port, Security: a.Security,
		User: a.User, From: a.From, To: a.To, ChatID: a.ChatID, BaseURL: a.BaseURL,
	}, &notifier.Secrets{Secret: a.Secret, Key: a.Key})
	var ie *notifier.InvalidError
	if err != nil {
		if errors.As(err, &ie) {
			return nil, control.NewError(control.CodeInvalid, ie.Text)
		}
		return nil, control.NewError(control.CodeInvalid, "the channel is not valid")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, err := n.settings.Get(c.Name); err == nil {
		return nil, control.NewError(control.CodeChannelExists, "a channel of this name exists; remove it first")
	} else if !errors.Is(err, notifier.ErrNotFound) {
		n.log.Error("notification settings not readable", "error", err)
		return nil, errNotifyUnreadable
	}
	// A leftover of an interrupted removal must not outlive the new channel.
	n.dropCredentials(c.Name)
	if a.Secret != "" {
		if err := n.creds.Set(notifier.SecretName(c.Name), a.Secret); err != nil {
			return nil, n.credentialFailure(err)
		}
	}
	if a.Key != "" {
		if err := n.creds.Set(notifier.KeyName(c.Name), a.Key); err != nil {
			n.dropCredentials(c.Name)
			return nil, n.credentialFailure(err)
		}
	}
	if err := n.settings.Add(c); err != nil {
		n.dropCredentials(c.Name)
		if errors.As(err, &ie) {
			return nil, control.NewError(control.CodeInvalid, ie.Text)
		}
		n.log.Error("notification settings not stored", "error", err)
		return nil, errNotifyUnreadable
	}
	return channelInfo(c), nil
}

func (n *notifyService) credentialFailure(err error) error {
	if errors.Is(err, credentials.ErrInvalidValue) || errors.Is(err, credentials.ErrInvalidName) {
		return control.NewError(control.CodeInvalid, err.Error())
	}
	n.log.Error("notification credential not stored", "error", err)
	return errNotifyCredentials
}

// dropCredentials removes the secret parts of a channel; what is not there is no failure.
func (n *notifyService) dropCredentials(name string) {
	for _, cred := range []string{notifier.SecretName(name), notifier.KeyName(name)} {
		if err := n.creds.Delete(cred); err != nil && !errors.Is(err, credentials.ErrNotFound) {
			n.log.Warn("notification credential not removed", "channel", name, "error", err)
		}
	}
}

func (n *notifyService) handleRemove(_ context.Context, raw json.RawMessage) (any, error) {
	var a control.NotifyNameArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := n.settings.Remove(a.Name); err != nil {
		if errors.Is(err, notifier.ErrNotFound) {
			return nil, errNoSuchChannel
		}
		n.log.Error("notification settings not stored", "error", err)
		return nil, errNotifyUnreadable
	}
	n.dropCredentials(a.Name)
	return struct{}{}, nil
}

var errNoSuchChannel = control.NewError(control.CodeNoSuchChannel, "no such channel")

// handleTest sends one test message and waits for the outcome. A failed delivery is answered with the error
// code of the notification kit and its redacted message.
func (n *notifyService) handleTest(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.NotifyNameArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	c, err := n.settings.Get(a.Name)
	switch {
	case errors.Is(err, notifier.ErrNotFound):
		return nil, errNoSuchChannel
	case err != nil:
		n.log.Error("notification settings not readable", "error", err)
		return nil, errNotifyUnreadable
	}
	ctx, cancel := context.WithTimeout(ctx, control.NotifyTestTimeout)
	defer cancel()
	if err := notifier.Send(ctx, n.test, n.creds, c, notifier.TestMessage()); err != nil {
		if errors.Is(err, notifier.ErrCredentials) {
			n.log.Error("notification credentials not readable", "channel", c.Name)
			return nil, errNotifyCredentials
		}
		return nil, control.NewError(notifier.ErrorCode(err), n.secrets.Apply(err.Error()))
	}
	return control.NotifyTestResult{Name: c.Name, Type: c.Type}, nil
}
