//go:build e2efakes

package main

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// runMail is a minimal SMTP server with STARTTLS and AUTH PLAIN/LOGIN that accepts every login and records
// each message as a JSON line; the password of a login is never stored.
func runMail() error {
	logPath := env("MAIL_LOG", "/data/messages.jsonl")
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	var mu sync.Mutex
	ln, err := net.Listen("tcp", ":587")
	if err != nil {
		return err
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer func() { _ = conn.Close() }()
			_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
			msg := serveSMTP(conn, cfg)
			if msg == nil {
				return
			}
			line, _ := json.Marshal(msg)
			mu.Lock()
			defer mu.Unlock()
			if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				_, _ = f.Write(append(line, '\n'))
				_ = f.Close()
			}
		}()
	}
}

// serveSMTP speaks one session and returns the message it received, or nil.
func serveSMTP(conn net.Conn, cfg *tls.Config) map[string]any {
	var c net.Conn = conn
	r := bufio.NewReader(c)
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(c, format+"\r\n", a...) }
	read := func() (string, bool) {
		line, err := r.ReadString('\n')
		return strings.TrimRight(line, "\r\n"), err == nil
	}
	secured, authed := false, false
	user := ""
	msg := map[string]any{}
	say("220 mail.qdeploye2e.test ESMTP")
	for {
		line, ok := read()
		if !ok {
			return nil
		}
		verb, rest, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO", "HELO":
			say("250-mail.qdeploye2e.test")
			if !secured {
				say("250-STARTTLS")
			} else {
				say("250-AUTH PLAIN LOGIN")
			}
			say("250 8BITMIME")
		case "STARTTLS":
			say("220 ready to start TLS")
			t := tls.Server(conn, cfg)
			if err := t.Handshake(); err != nil {
				return nil
			}
			c, r, secured = t, bufio.NewReader(t), true
		case "AUTH":
			if !secured {
				say("530 5.7.0 must issue a STARTTLS command first")
				continue
			}
			mech, initial, _ := strings.Cut(rest, " ")
			switch strings.ToUpper(mech) {
			case "PLAIN":
				if initial == "" {
					say("334 ")
					initial, _ = read()
				}
				raw, _ := base64.StdEncoding.DecodeString(initial)
				parts := strings.Split(string(raw), "\x00")
				if len(parts) == 3 {
					user = parts[1]
				}
			case "LOGIN":
				say("334 VXNlcm5hbWU6")
				u, _ := read()
				raw, _ := base64.StdEncoding.DecodeString(u)
				user = string(raw)
				say("334 UGFzc3dvcmQ6")
				_, _ = read()
			default:
				say("504 unsupported mechanism")
				continue
			}
			authed = true
			say("235 2.7.0 authenticated")
		case "MAIL":
			if !authed {
				say("530 5.7.0 authentication required")
				continue
			}
			msg["from"] = rest
			say("250 ok")
		case "RCPT":
			msg["to"] = append(toList(msg["to"]), rest)
			say("250 ok")
		case "DATA":
			say("354 end with <CRLF>.<CRLF>")
			var b strings.Builder
			for {
				l, ok := read()
				if !ok {
					return nil
				}
				if l == "." {
					break
				}
				b.WriteString(strings.TrimPrefix(l, ".") + "\n")
			}
			msg["data"], msg["user"], msg["time"] = b.String(), user, time.Now().UTC().Format(time.RFC3339Nano)
			say("250 2.0.0 queued")
		case "QUIT":
			say("221 bye")
			if msg["data"] == nil {
				return nil
			}
			return msg
		default:
			say("502 command not implemented")
		}
	}
}

func toList(v any) []string {
	l, _ := v.([]string)
	return l
}
