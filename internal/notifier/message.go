package notifier

import (
	"encoding/json"
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
	// Kind, Container and Since describe a container failure, for EventContainerFailed and EventRecovered; on
	// a recovery Kind is the kind of the ended failure. Time is when the transition was found.
	Kind, Container string
	Since, Time     time.Time
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
	if ev.Type == EventContainerFailed || ev.Type == EventRecovered {
		b.Kind, b.Container, b.Since = ev.Kind, ev.Container, utc(ev.Since)
		if ev.Type == EventRecovered {
			b.At = utc(ev.Time)
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
