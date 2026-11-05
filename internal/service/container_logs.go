package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"time"

	"github.com/castrowithcee/qatlas-kit/docker"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-deploy/internal/backup"
	"github.com/castrowithcee/qatlas-deploy/internal/control"
)

const (
	// logsBudget bounds the encoded lines of one answer, well below control.MaxResponseBytes.
	logsBudget = 640 << 10
	// logsLineBytes bounds one line as read; the encoded line may still grow by masking and escaping.
	logsLineBytes = 8 << 10
	// logsRate lifts the kit's default rate limit of 1000 lines per second, which would slow a page of short lines.
	logsRate = 1_000_000
)

// LogReader is the stream of log lines the service reads; *docker.LogStream implements it.
type LogReader interface {
	Next() (docker.LogLine, error)
	Close() error
}

// engineClient adds Logs to the kit client, whose stream is a concrete type.
type engineClient struct{ *docker.Client }

func (c engineClient) Logs(ctx context.Context, ref string, opts docker.LogOptions) (LogReader, error) {
	st, err := c.Client.Logs(ctx, ref, opts)
	if err != nil {
		return nil, err
	}
	return st, nil
}

// logSource is one container's stream with its next unread line.
type logSource struct {
	name   string
	stream LogReader
	head   *docker.LogLine
	done   bool
}

type logEntry struct {
	line docker.LogLine
	name string
}

func (s *containerService) handleLogs(ctx context.Context, raw json.RawMessage) (any, error) {
	var a control.ContainerLogsArgs
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if (a.Name == "") == (a.Stack == "") {
		return nil, control.NewError(control.CodeBadRequest, "exactly one of a container name and a stack is required")
	}
	since := a.Since
	if a.Cursor.After(since) {
		since = a.Cursor
	}
	if a.Tail < 0 || a.Tail > control.ContainerLogsMaxTail || a.Tail > 0 && !a.Cursor.IsZero() ||
		!since.IsZero() && since.Unix() < 0 || !a.Until.IsZero() && (a.Until.Unix() < 0) ||
		a.Stack != "" && !backup.ValidStack(a.Stack) {
		return nil, control.NewError(control.CodeInvalid, "the log arguments are not valid")
	}
	// Stack secrets are registered before any line is read, also those of stacks never deployed since start.
	dirs := s.stackDirs()

	type target struct{ id, name string }
	var targets []target
	if a.Name != "" {
		d, err := s.docker.Inspect(ctx, a.Name)
		if err != nil {
			return nil, s.dockerAnswer(err)
		}
		targets = append(targets, target{d.ID, d.Name})
	} else {
		if _, ok := dirs[a.Stack]; !ok {
			return nil, control.NewError(control.CodeNoSuchStack, "no such stack in the working copy")
		}
		all, err := s.docker.List(ctx)
		if err != nil {
			return nil, s.dockerAnswer(err)
		}
		for _, c := range all {
			kind, _ := classify(c.Labels["com.docker.compose.project"], c.Labels["com.docker.compose.project.working_dir"], dirs)
			if kind == control.ContainerKindOwn && c.Labels["com.docker.compose.project"] == a.Stack {
				targets = append(targets, target{c.ID, c.Name})
			}
		}
		sort.Slice(targets, func(i, j int) bool { return targets[i].name < targets[j].name })
	}

	res := control.ContainerLogsResult{Lines: []control.ContainerLogLine{}, Cursor: a.Cursor}
	if !a.Until.IsZero() && a.Until.Before(since) {
		return res, nil
	}
	ctx, cancel := context.WithTimeout(ctx, control.ContainerLogsTimeout-5*time.Second)
	defer cancel()
	opts := docker.LogOptions{Since: since, Until: a.Until, Tail: a.Tail, MaxLineBytes: logsLineBytes, MaxLinesPerSecond: logsRate}
	var sources []*logSource
	defer func() {
		for _, src := range sources {
			_ = src.stream.Close()
		}
	}()
	for _, t := range targets {
		st, err := s.docker.Logs(ctx, t.id, opts)
		if err != nil {
			var de *docker.Error
			if a.Stack != "" && errors.As(err, &de) && de.Code == output.CodeNotFound {
				continue // removed since the list
			}
			return nil, s.dockerAnswer(err)
		}
		sources = append(sources, &logSource{name: t.name, stream: st})
	}

	// pull returns the earliest unread line over all sources; equal times keep the order of the names.
	pull := func() (logEntry, bool, error) {
		best := -1
		for i, src := range sources {
			for src.head == nil && !src.done {
				l, err := src.stream.Next()
				if errors.Is(err, io.EOF) {
					src.done = true
				} else if err != nil {
					return logEntry{}, false, s.dockerAnswer(err)
				} else if !l.Time.Before(since) && (a.Until.IsZero() || !l.Time.After(a.Until)) {
					src.head = &l
				}
			}
			if src.head != nil && (best < 0 || src.head.Time.Before(sources[best].head.Time)) {
				best = i
			}
		}
		if best < 0 {
			return logEntry{}, false, nil
		}
		src := sources[best]
		e := logEntry{line: *src.head, name: src.name}
		src.head = nil
		return e, true, nil
	}
	if a.Tail > 0 {
		// The last n lines of the merge are known only after reading all; each source holds at most n lines.
		var all []logEntry
		for {
			e, ok, err := pull()
			if err != nil {
				return nil, err
			}
			if !ok {
				break
			}
			all = append(all, e)
		}
		if len(all) > a.Tail {
			all = all[len(all)-a.Tail:]
		}
		pull = func() (logEntry, bool, error) {
			if len(all) == 0 {
				return logEntry{}, false, nil
			}
			e := all[0]
			all = all[1:]
			return e, true, nil
		}
	}

	used := 0
	for {
		e, ok, err := pull()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		line := control.ContainerLogLine{Time: e.line.Time.UTC(), Source: s.text(e.name), Stream: string(e.line.Source),
			Text: s.text(e.line.Text), Truncated: e.line.Truncated}
		b, err := json.Marshal(line)
		if err != nil {
			return nil, err
		}
		if used+len(b)+1 > logsBudget && len(res.Lines) > 0 {
			res.More = true
			// Lines of one time are never split between answers, or the cursor would skip the rest of them. A
			// group larger than the whole budget cannot be kept together and loses its remainder.
			n := len(res.Lines)
			for n > 0 && res.Lines[n-1].Time.Equal(line.Time) {
				n--
			}
			if n > 0 {
				res.Lines = res.Lines[:n]
			}
			break
		}
		used += len(b) + 1
		res.Lines = append(res.Lines, line)
	}
	if n := len(res.Lines); n > 0 {
		res.Cursor = res.Lines[n-1].Time.Add(time.Nanosecond)
	}
	return res, nil
}
