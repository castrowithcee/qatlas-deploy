//go:build e2efakes

package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// runDemoApp is the application inside the demo images in the test registry. It answers GET / with its name
// and version and GET /health with 200, or with 500 when DEMO_HEALTH=fail, which is how a stack is made
// to turn unhealthy.
func runDemoApp() error {
	name, version := env("DEMO_NAME", "demo"), env("DEMO_VERSION", "unknown")
	healthy := env("DEMO_HEALTH", "ok") != "fail"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		if !healthy {
			http.Error(w, "unhealthy", http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s %s\n", name, version)
	})
	return http.ListenAndServe(env("DEMO_ADDR", ":8080"), mux)
}

// runHealthcheck is the HEALTHCHECK command of the demo image, which has no shell or curl.
func runHealthcheck() error {
	addr := env("DEMO_ADDR", ":8080")
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health answered %s", resp.Status)
	}
	return nil
}
