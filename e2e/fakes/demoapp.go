//go:build e2efakes

package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// runDemoApp is the application inside the demo images in the test registry. It answers GET / with its name
// and version and GET /health with 200, or with 500 when DEMO_HEALTH=fail, which is how a stack is made
// to turn unhealthy. It writes a start line (with its DEMO_SECRET, so the log tests can prove that the value is
// masked), a line to stderr and a tick line every two seconds to its log. A file /data/break keeps /health at
// 500 until it is removed; /data/break-once does the same until the app starts again, so a restart ends it.
// With DEMO_FAIL_AFTER set to a duration it writes /data/late-failed that long after its start, and /health stays
// at 500 while that file exists, also after a restart, so only data from before the failure ends it.
func runDemoApp() error {
	name, version := env("DEMO_NAME", "demo"), env("DEMO_VERSION", "unknown")
	fmt.Printf("demo started name=%s version=%s token=%s\n", name, version, os.Getenv("DEMO_SECRET"))
	fmt.Fprintln(os.Stderr, "demo warning on stderr")
	go func() {
		for range time.Tick(2 * time.Second) {
			fmt.Println("demo tick")
		}
	}()
	_ = os.Remove("/data/break-once")
	if d, err := time.ParseDuration(os.Getenv("DEMO_FAIL_AFTER")); err == nil && d > 0 {
		go func() {
			time.Sleep(d)
			_ = os.WriteFile("/data/late-failed", []byte("failed\n"), 0o644)
		}()
	}
	healthy := env("DEMO_HEALTH", "ok") != "fail"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, broken := os.Stat("/data/break")
		_, brokenOnce := os.Stat("/data/break-once")
		_, late := os.Stat("/data/late-failed")
		if !healthy || broken == nil || brokenOnce == nil || late == nil {
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
