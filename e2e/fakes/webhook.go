//go:build e2efakes

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// runWebhook records every request except GET /_e2e/requests, which returns the recorded ones as JSON
// lines, and answers 204. Authorization and Cookie header values are replaced before storing.
func runWebhook() error {
	logPath := env("WEBHOOK_LOG", "/data/requests.jsonl")
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/_e2e/requests", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		data, _ := os.ReadFile(logPath)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		headers := map[string]string{}
		for k, v := range r.Header {
			val := strings.Join(v, ", ")
			if k == "Authorization" || k == "Cookie" {
				val = "[redacted]"
			}
			headers[k] = val
		}
		line, _ := json.Marshal(map[string]any{
			"time": time.Now().UTC().Format(time.RFC3339Nano), "method": r.Method, "path": r.URL.RequestURI(),
			"headers": headers, "body": string(body),
		})
		mu.Lock()
		defer mu.Unlock()
		if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.Write(append(line, '\n'))
			_ = f.Close()
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return serveTLS(":443", mux)
}
