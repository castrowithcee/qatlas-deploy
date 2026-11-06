package notifier

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	kitnotify "github.com/castrowithcee/qatlas-kit/notify"
)

// Event is one thing to report. Type is one of the Event constants; Error is already redacted.
type Event struct {
	Type   string
	Stack  string
	Run    string
	Commit string
	Error  string
	// Items are the variables with a new version, for EventUpdateAvailable.
	Items []Item
	// Kind, Container and Since describe a container failure, for EventContainerFailed, EventRecovered and
	// EventRecoveryFailed; on a recovery Kind is the kind of the ended failure. Time is when the transition was
	// found.
	Kind, Container string
	Since, Time     time.Time
	// Restarted is set on a recovery that followed a restart by the repair.
	Restarted bool
	// Attempts is how many restarts did not help, for EventRecoveryFailed.
	Attempts int
}

// Item is one image variable with an available version.
type Item struct {
	Variable  string `json:"variable"`
	Current   string `json:"current,omitempty"`
	Available string `json:"available"`
}

// body is the JSON a webhook receives. The field names are stable.
type body struct {
	Event  string `json:"event"`
	Stack  string `json:"stack,omitempty"`
	Run    string `json:"run,omitempty"`
	Commit string `json:"commit,omitempty"`
	Error  string `json:"error,omitempty"`
	Items  []Item `json:"items,omitempty"`
	// The fields of a container failure; since and at are RFC 3339 times in UTC.
	Kind      string `json:"kind,omitempty"`
	Container string `json:"container,omitempty"`
	Since     string `json:"since,omitempty"`
	At        string `json:"at,omitempty"`
	Restarted bool   `json:"restarted,omitempty"`
	Attempts  int    `json:"attempts,omitempty"`
}

const shortCommit = 12

func short(commit string) string {
	if len(commit) > shortCommit {
		return commit[:shortCommit]
	}
	return commit
}

func utc(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// noun names what failed: a missing one is a Compose service, the others are containers.
func noun(kind string) string {
	if kind == "missing" {
		return "Service"
	}
	return "Container"
}

func nameSuffix(name string) string {
	if name == "" {
		return ""
	}
	return " " + name
}

func failurePhrase(kind string) string {
	switch kind {
	case "exited":
		return "has exited"
	case "unhealthy":
		return "is unhealthy"
	case "restart-loop":
		return "is restarting in a loop"
	case "missing":
		return "is missing"
	}
	return "failed (" + kind + ")"
}

// Message writes the notification of an event: a subject, a short text and the JSON body of a webhook.
func Message(ev Event) kitnotify.Message {
	var subject string
	var text []string
	switch ev.Type {
	case EventSucceeded:
		subject = "Stack " + ev.Stack + " deployed"
		text = append(text, "Run "+ev.Run+" deployed commit "+short(ev.Commit)+".")
	case EventFailed:
		subject = "Stack " + ev.Stack + " failed"
		text = append(text, "Run "+ev.Run+" failed at commit "+short(ev.Commit)+".")
	case EventRolledBack:
		subject = "Stack " + ev.Stack + " rolled back"
		text = append(text, "Run "+ev.Run+" failed at commit "+short(ev.Commit)+" and the stack was rolled back.")
	case EventRollbackFailed:
		subject = "Stack " + ev.Stack + ": rollback failed"
		text = append(text, "Run "+ev.Run+" failed at commit "+short(ev.Commit)+" and the rollback failed too.",
			"The stack is stopped and gets no further runs until an operator acts.")
	case EventContainerFailed:
		subject = "Stack " + ev.Stack + ": " + noun(ev.Kind) + nameSuffix(ev.Container) + " failed (" + ev.Kind + ")"
		text = append(text, noun(ev.Kind)+nameSuffix(ev.Container)+" of stack "+ev.Stack+" "+failurePhrase(ev.Kind)+".",
			"Since: "+utc(ev.Since))
	case EventRecovered:
		subject = "Stack " + ev.Stack + ": " + noun(ev.Kind) + nameSuffix(ev.Container) + " recovered"
		text = append(text, noun(ev.Kind)+nameSuffix(ev.Container)+" of stack "+ev.Stack+" is fine again; it had failed ("+ev.Kind+").",
			"Failed since: "+utc(ev.Since)+" (down for "+ev.Time.Sub(ev.Since).Round(time.Second).String()+")")
		if ev.Restarted {
			text = append(text, "A restart by qdeploy fixed it.")
		}
	case EventRecoveryFailed:
		subject = "Stack " + ev.Stack + ": " + noun(ev.Kind) + nameSuffix(ev.Container) + " not repaired"
		text = append(text, noun(ev.Kind)+nameSuffix(ev.Container)+" of stack "+ev.Stack+" "+failurePhrase(ev.Kind)+
			" and "+strconv.Itoa(ev.Attempts)+" restarts did not fix it.",
			"No further restart is made until an operator resumes the stack.", "Failed since: "+utc(ev.Since))
	case EventUpdateAvailable:
		subject = "Update available for stack " + ev.Stack
		for _, it := range ev.Items {
			line := it.Variable + ": "
			if it.Current != "" {
				line += it.Current + " -> "
			}
			text = append(text, line+it.Available)
		}
	default:
		subject = "Event " + ev.Type + " for stack " + ev.Stack
	}
	if ev.Error != "" {
		text = append(text, "Error: "+ev.Error)
	}
	b := body{Event: ev.Type, Stack: ev.Stack, Run: ev.Run, Commit: ev.Commit, Error: ev.Error, Items: ev.Items}
	switch ev.Type {
	case EventContainerFailed, EventRecovered, EventRecoveryFailed:
		b.Kind, b.Container, b.Since = ev.Kind, ev.Container, utc(ev.Since)
		switch ev.Type {
		case EventRecovered:
			b.At, b.Restarted = utc(ev.Time), ev.Restarted
		case EventRecoveryFailed:
			b.At, b.Attempts = utc(ev.Time), ev.Attempts
		}
	}
	raw, _ := json.Marshal(b)
	return kitnotify.Message{Event: ev.Type, Subject: subject, Text: strings.Join(text, "\n"), JSON: raw}
}

// TestMessage is what a test sends.
func TestMessage() kitnotify.Message {
	text := "This is a test message from the qdeploy service. If you can read it, the channel works."
	raw, _ := json.Marshal(struct {
		Event string `json:"event"`
		Text  string `json:"text"`
	}{"test", text})
	return kitnotify.Message{Event: "test", Subject: "qdeploy test message", Text: text, JSON: raw}
}
