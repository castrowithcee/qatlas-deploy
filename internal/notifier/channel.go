// Package notifier holds the notification channels of the service: their settings, the texts of the events,
// the once-per-version record of update notices and the dispatcher that hands events to the notification kit.
// The secret parts of a channel live in the credential store, never in its settings.
package notifier

import (
	"errors"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	kitnotify "github.com/castrowithcee/qatlas-kit/notify"

	"github.com/castrowithcee/qatlas-deploy/internal/credentials"
)

// The types of a channel.
const (
	TypeWebhook  = "webhook"
	TypeSlack    = "slack"
	TypeTelegram = "telegram"
	TypeSMTP     = "smtp"
)

// The events a channel can select. The first four are the result words of a run; container-failed, recovered
// and recovery-failed come from the failure monitor.
const (
	EventSucceeded       = "succeeded"
	EventFailed          = "failed"
	EventRolledBack      = "rolled-back"
	EventRollbackFailed  = "rollback-failed"
	EventContainerFailed = "container-failed"
	EventRecovered       = "recovered"
	EventRecoveryFailed  = "recovery-failed"
	EventUpdateAvailable = "update-available"
)

// AllEvents lists every event in the order settings and answers show them.
var AllEvents = []string{EventSucceeded, EventFailed, EventRolledBack, EventRollbackFailed, EventContainerFailed, EventRecovered,
	EventRecoveryFailed, EventUpdateAvailable}

// The security of an SMTP channel.
const (
	SecurityStartTLS = "starttls"
	SecurityTLS      = "tls"
)

// The headers of a webhook request. The body is signed as described by the kit's Sign function.
const (
	HeaderEvent     = "X-Qdeploy-Event"
	HeaderTimestamp = "X-Qdeploy-Timestamp"
	HeaderSignature = "X-Qdeploy-Signature"
)

// MaxChannels bounds how many channels a host has.
const MaxChannels = 16

const (
	maxRecipients = 10
	maxFieldLen   = 256
)

// Channel is the non-secret setting of one channel.
type Channel struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Events   []string `json:"events"`
	Host     string   `json:"host,omitempty"`
	Port     int      `json:"port,omitempty"`
	Security string   `json:"security,omitempty"`
	User     string   `json:"user,omitempty"`
	From     string   `json:"from,omitempty"`
	To       []string `json:"to,omitempty"`
	ChatID   string   `json:"chat_id,omitempty"`
	BaseURL  string   `json:"base_url,omitempty"`
}

// Secrets are the secret parts a channel is added with. Secret is the URL of a webhook or Slack channel, the
// token of a Telegram bot or the password of an SMTP account; Key is the signing key of a webhook.
type Secrets struct {
	Secret string
	Key    string
}

// InvalidError is a refused setting; its text is fixed and never contains a value of the request.
type InvalidError struct{ Text string }

func (e *InvalidError) Error() string { return e.Text }

func invalid(text string) error { return &InvalidError{Text: text} }

var (
	nameRe   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	hostRe   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	chatIDRe = regexp.MustCompile(`^(-?[0-9]{1,20}|@[A-Za-z][A-Za-z0-9_]{3,31})$`)
)

// ValidName reports whether name may name a channel. It is short enough for the credential names derived
// from it and has no dot, so one channel's names never collide with another's.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// SecretName is the credential that holds the secret of the channel.
func SecretName(channel string) string { return "notify." + channel }

// KeyName is the credential that holds the signing key of a webhook channel.
func KeyName(channel string) string { return "notify." + channel + ".key" }

// NormalizeEvents checks the selected events and returns them in canonical order without repeats; none selected
// means all.
func NormalizeEvents(events []string) ([]string, error) {
	if len(events) == 0 {
		return slices.Clone(AllEvents), nil
	}
	var out []string
	for _, e := range AllEvents {
		if slices.Contains(events, e) {
			out = append(out, e)
		}
	}
	for _, e := range events {
		if !slices.Contains(AllEvents, e) {
			return nil, invalid("an event is not one of succeeded, failed, rolled-back, rollback-failed, container-failed, recovered, recovery-failed, update-available")
		}
	}
	return out, nil
}

// Subscribes tells whether the channel selected the event.
func (c Channel) Subscribes(event string) bool { return slices.Contains(c.Events, event) }

// Validate checks the setting of a channel, with its secrets when they are given (add), and fills the defaults
// of the type. It never echoes a value.
func Validate(c Channel, s *Secrets) (Channel, error) {
	if !ValidName(c.Name) {
		return c, invalid("the channel name must be 1 to 32 characters: lowercase letters, digits or '-', starting with a letter")
	}
	events, err := NormalizeEvents(c.Events)
	if err != nil {
		return c, err
	}
	c.Events = events
	var secret, key string
	if s != nil {
		secret, key = s.Secret, s.Key
	}
	has := func(vals ...any) bool {
		for _, v := range vals {
			switch x := v.(type) {
			case string:
				if x != "" {
					return true
				}
			case int:
				if x != 0 {
					return true
				}
			case []string:
				if len(x) > 0 {
					return true
				}
			}
		}
		return false
	}
	smtpFields := has(c.Host, c.Port, c.Security, c.User, c.From, c.To)
	telegramFields := has(c.ChatID, c.BaseURL)
	switch c.Type {
	case TypeWebhook, TypeSlack:
		if smtpFields || telegramFields {
			return c, invalid("the host, port, security, user, from, to, chat and base URL settings do not belong to this channel type")
		}
		if s != nil {
			if err := checkURL(secret, "the URL", false); err != nil {
				return c, err
			}
			if c.Type == TypeWebhook {
				if err := credentials.ValidateValue(key); err != nil {
					return c, invalid("a webhook needs a signing key of 4 to 8192 bytes of text")
				}
			} else if key != "" {
				return c, invalid("a signing key belongs to a webhook only")
			}
		}
	case TypeTelegram:
		if smtpFields || key != "" {
			return c, invalid("the host, port, security, user, from, to and key settings do not belong to this channel type")
		}
		if s != nil {
			if err := credentials.ValidateValue(secret); err != nil {
				return c, invalid("a Telegram channel needs the bot token as its secret")
			}
		}
		if !chatIDRe.MatchString(c.ChatID) {
			return c, invalid("the chat must be a number or @name")
		}
		if c.BaseURL != "" {
			if err := checkURL(c.BaseURL, "the base URL", true); err != nil {
				return c, err
			}
		}
	case TypeSMTP:
		if telegramFields || key != "" {
			return c, invalid("the chat, base URL and key settings do not belong to this channel type")
		}
		if !hostRe.MatchString(c.Host) {
			return c, invalid("the SMTP host is not valid")
		}
		if c.Port < 0 || c.Port > 65535 {
			return c, invalid("the SMTP port must be from 1 to 65535")
		}
		switch c.Security {
		case "":
			c.Security = SecurityStartTLS
		case SecurityStartTLS, SecurityTLS:
		default:
			return c, invalid("the SMTP security must be starttls or tls")
		}
		if !plain(c.User) || len(c.User) > maxFieldLen {
			return c, invalid("the SMTP user is not valid")
		}
		if err := checkAddress(c.From); err != nil {
			return c, invalid("the sender address is not valid")
		}
		if len(c.To) == 0 || len(c.To) > maxRecipients {
			return c, invalid("name one to ten recipients")
		}
		for _, to := range c.To {
			if checkAddress(to) != nil {
				return c, invalid("a recipient address is not valid")
			}
		}
		if s != nil {
			switch {
			case c.User != "" && credentials.ValidateValue(secret) != nil:
				return c, invalid("an SMTP login needs a password of 4 to 8192 bytes of text")
			case c.User == "" && secret != "":
				return c, invalid("an SMTP password needs a user")
			}
		}
	default:
		return c, invalid("the channel type must be webhook, slack, telegram or smtp")
	}
	return c, nil
}

// plain reports whether s is valid text without control characters.
func plain(s string) bool {
	return utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}

func checkAddress(s string) error {
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || len(s) > 254 {
		return errors.New("address")
	}
	return nil
}

// checkURL accepts an absolute http or https address without user information. The kit decides at delivery
// which hosts may use plain http. A base URL may not carry a query or fragment.
func checkURL(raw, what string, base bool) error {
	bad := invalid(what + " must be an absolute https address without user information")
	if raw == "" || len(raw) > credentials.MaxValueBytes || !plain(raw) {
		return bad
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return bad
	}
	// The kit sends plain http only to the local machine; refusing it here keeps a channel from being stored
	// that could never deliver.
	if u.Scheme != "https" && (u.Scheme != "http" || !loopback(u.Hostname())) {
		return bad
	}
	if base && (u.RawQuery != "" || len(raw) > maxFieldLen) {
		return bad
	}
	return nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CredentialReader returns the value of a stored credential.
type CredentialReader interface {
	Value(name string) (string, error)
}

// ErrCredentials means the secret parts of a channel cannot be read. Its text names no credential.
var ErrCredentials = errors.New("the credentials of the channel cannot be read")

// Kit returns the channel as the notification kit delivers it.
func Kit(c Channel, creds CredentialReader) (kitnotify.Channel, error) {
	read := func(name string) (string, error) {
		v, err := creds.Value(name)
		if err != nil {
			return "", ErrCredentials
		}
		return v, nil
	}
	switch c.Type {
	case TypeWebhook:
		u, err := read(SecretName(c.Name))
		if err != nil {
			return nil, err
		}
		k, err := read(KeyName(c.Name))
		if err != nil {
			return nil, err
		}
		return kitnotify.Webhook{URL: u, Key: k, Headers: kitnotify.WebhookHeaders{
			Event: HeaderEvent, Timestamp: HeaderTimestamp, Signature: HeaderSignature,
		}}, nil
	case TypeSlack:
		u, err := read(SecretName(c.Name))
		if err != nil {
			return nil, err
		}
		return kitnotify.Slack{URL: u}, nil
	case TypeTelegram:
		t, err := read(SecretName(c.Name))
		if err != nil {
			return nil, err
		}
		return kitnotify.Telegram{Token: t, ChatID: c.ChatID, BaseURL: c.BaseURL}, nil
	case TypeSMTP:
		var password string
		if c.User != "" {
			var err error
			if password, err = read(SecretName(c.Name)); err != nil {
				return nil, err
			}
		}
		sec := kitnotify.StartTLS
		if c.Security == SecurityTLS {
			sec = kitnotify.ImplicitTLS
		}
		return kitnotify.SMTP{Host: c.Host, Port: c.Port, Security: sec, User: c.User, Password: password,
			From: c.From, To: slices.Clone(c.To)}, nil
	}
	return nil, errors.New("unknown channel type")
}
