//go:build e2efakes

// Command fakes provides the helper services of the end-to-end environment: a GitHub releases fake with
// release downloads, a webhook receiver, a mail server and the application of the demo images. It is built only with
// -tags e2efakes, so the normal build, vet and test runs never see it.
package main

import (
	"fmt"
	"os"
)

const usage = "usage: fakes releases|webhook|mail|demoapp|healthcheck"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "releases":
		err = runReleases()
	case "webhook":
		err = runWebhook()
	case "mail":
		err = runMail()
	case "demoapp":
		err = runDemoApp()
	case "healthcheck":
		err = runHealthcheck()
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakes:", err)
		os.Exit(1)
	}
}
