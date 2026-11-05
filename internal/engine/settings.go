package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/castrowithcee/qatlas-deploy/internal/gitrepo"
	"github.com/castrowithcee/qatlas-deploy/internal/stacks"
)

// ErrInvalidSettings means new settings do not fit the stack, such as an image variable it does not have or
// compose files that are missing or give it another project name. The error carries the reason.
var ErrInvalidSettings = errors.New("the settings do not fit the stack")

// StackSettings returns the settings of the stack as the working copy has them, nil when it has none. A stack
// that cannot be read is refused with ErrUnreadable.
func (e *Engine) StackSettings(ctx context.Context, name string) (*stacks.Rules, error) {
	b, err := e.cfg.Source()
	if err != nil {
		return nil, err
	}
	if err := e.lockCommand(ctx); err != nil {
		return nil, err
	}
	defer e.lock.Unlock()
	list, err := stacks.Load(b.Dir)
	if err != nil {
		return nil, fmt.Errorf("read stacks: %w", err)
	}
	i := slices.IndexFunc(list, func(s stacks.Stack) bool { return s.Name == name })
	switch {
	case i < 0:
		return nil, ErrUnknownStack
	case list[i].Err != nil:
		return nil, fmt.Errorf("%w: %s", ErrUnreadable, e.text(list[i].Err.Error()))
	}
	return list[i].Rules, nil
}

// SetSettings makes rules the settings of the stack: a commit that writes them to the stack's settings file
// (stacks.SettingsPath) in the form of stacks.EncodeSettings, pushed to the branch, and the working copy
// fast-forwarded to it. The commit is built on the freshly fetched branch without touching the working copy;
// the working copy follows the branch first, so the rules are checked against the stack the commit belongs
// to. A push the remote rejects because the branch moved is built and pushed once more. It returns the
// commit, or "" when the stack already has these settings and nothing was committed.
//
// Like an update's commit it waits until no run relies on the working copy and holds the working-copy lock
// and the ref lock to its end; a command that waits longer than LockWait gets ErrBusy. Rules that do not fit
// the stack are refused with ErrInvalidSettings, a stack that cannot be read for another reason with
// ErrUnreadable, a working copy with local changes with ErrLocalChanges or ErrCopyChanges.
func (e *Engine) SetSettings(ctx context.Context, name string, rules *stacks.Rules) (string, error) {
	if rules == nil {
		return "", errors.New("no settings given")
	}
	b, err := e.cfg.Source()
	if err != nil {
		return "", err
	}
	content, err := stacks.EncodeSettings(rules)
	if err != nil {
		return "", err
	}
	wctx, cancel := context.WithTimeout(ctx, e.cfg.LockWait)
	err = e.exclusive(wctx)
	cancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return "", ErrBusy
		}
		return "", err
	}
	defer e.lock.Unlock()
	e.refs.Lock()
	defer e.refs.Unlock()

	wc := b.Copy
	message := "qdeploy: settings of " + name
	edit := func(old []byte) ([]byte, error) {
		// Settings with the same meaning count as unchanged, whatever form the file has.
		if old != nil {
			if cur, err := stacks.ParseSettings(old); err == nil {
				if same, err := stacks.EncodeSettings(cur); err == nil && bytes.Equal(same, content) {
					return old, nil
				}
			}
		}
		return content, nil
	}
	for attempt := 1; ; attempt++ {
		if err := e.settingsBase(ctx, b, name, rules); err != nil {
			return "", err
		}
		c, err := wc.CommitFile(ctx, stacks.SettingsPath(name), edit, message)
		if errors.Is(err, gitrepo.ErrUnchanged) {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("commit: %w", err)
		}
		err = wc.Push(ctx, c)
		switch {
		case err == nil:
		case errors.Is(err, gitrepo.ErrPushRejected) && attempt == 1:
			continue
		case e.landed(ctx, wc, c):
			// The push may fail after the remote took the commit; the branch is what counts.
		default:
			return "", fmt.Errorf("push: %w; nothing was pushed", err)
		}
		e.cfg.Logger.Info("stack settings committed", "stack", name, "commit", c)
		if err := wc.FastForward(ctx, c); err != nil {
			return c, fmt.Errorf("the settings are on the branch, but the working copy did not follow: %w", err)
		}
		return c, nil
	}
}

// settingsBase fetches, moves the clean working copy to the branch and checks rules against the stack there.
// The caller has the working copy to itself and holds the ref lock.
func (e *Engine) settingsBase(ctx context.Context, b Binding, name string, rules *stacks.Rules) error {
	wc := b.Copy
	if err := wc.Fetch(ctx); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	changes, err := wc.Status(ctx)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	list, err := stacks.Load(b.Dir)
	if err != nil {
		return fmt.Errorf("read stacks: %w", err)
	}
	local, dirty := classify(changes, list)
	if folder := folderOf(list, name); len(local[folder]) > 0 {
		return fmt.Errorf("%w: %s", ErrLocalChanges, e.localDetail(local[folder]))
	}
	if dirty {
		return ErrCopyChanges
	}
	head, err := wc.Head(ctx)
	if err != nil {
		return err
	}
	remote, err := wc.RemoteHead(ctx)
	if err != nil {
		return err
	}
	if head != remote {
		if ok, err := wc.IsAncestor(ctx, head, remote); err != nil || !ok {
			return errors.Join(errors.New("the working copy has commits the branch lacks and cannot follow a new commit; nothing was pushed"), err)
		}
		if err := wc.FastForward(ctx, remote); err != nil {
			return fmt.Errorf("fast-forward: %w", err)
		}
		if list, err = stacks.Load(b.Dir); err != nil {
			return fmt.Errorf("read stacks: %w", err)
		}
	}
	i := slices.IndexFunc(list, func(s stacks.Stack) bool { return s.Name == name })
	if i < 0 {
		return ErrUnknownStack
	}
	st := list[i]
	if st.Err != nil && !stacks.IsSettingsError(st.Err) {
		return fmt.Errorf("%w: %s", ErrUnreadable, e.text(st.Err.Error()))
	}
	with, ok := stacks.WithSettings(b.Dir, st.Folder, rules)
	switch {
	case !ok:
		return fmt.Errorf("%w: the stack has no compose file with these settings", ErrInvalidSettings)
	case with.Err != nil && stacks.IsSettingsError(with.Err):
		return fmt.Errorf("%w: %s", ErrInvalidSettings, e.text(with.Err.Error()))
	case with.Err != nil:
		return fmt.Errorf("%w: %s", ErrUnreadable, e.text(with.Err.Error()))
	}
	return nil
}

// landed fetches and reports whether the branch is at commit c. The caller holds the ref lock.
func (e *Engine) landed(ctx context.Context, wc WorkingCopy, c string) bool {
	if wc.Fetch(ctx) != nil {
		return false
	}
	remote, err := wc.RemoteHead(ctx)
	return err == nil && remote == c
}
