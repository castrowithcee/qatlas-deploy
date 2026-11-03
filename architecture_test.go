package main

import (
	"os"
	"testing"

	"github.com/castrowithcee/qatlas-kit/archtest"
)

func TestArchitecture(t *testing.T) {
	archtest.Check(t, os.DirFS("."), []archtest.Rule{
		{
			Name:  "service-stays-below-the-command-surface",
			Files: archtest.Files{Under: []string{"internal/service"}, Tests: archtest.WithTests},
			Check: archtest.ForbidImports("internal/cli"),
			Hint:  "the command surface calls the service, never the other way round",
		},
		{
			Name:  "service-does-not-call-its-own-client",
			Files: archtest.Files{Under: []string{"internal/service"}, Tests: archtest.SkipTests},
			Check: archtest.ForbidImports("internal/client"),
			Hint:  "the service answers over the channel; the client is how others call it",
		},
		{
			Name:  "control-holds-the-protocol-only",
			Files: archtest.Files{Under: []string{"internal/control"}, Tests: archtest.WithTests},
			Check: archtest.ForbidImports("internal/service", "internal/client", "internal/cli"),
			Hint:  "the protocol sits below the service and the client and knows neither",
		},
		{
			Name:  "client-talks-protocol-only",
			Files: archtest.Files{Under: []string{"internal/client"}, Tests: archtest.WithTests},
			Check: archtest.ForbidImports("internal/service", "internal/cli"),
			Hint:  "the client reaches the service over the channel, never by import",
		},
		{
			Name:  "helptopics-import-nothing-of-the-module",
			Files: archtest.Files{Under: []string{"internal/helptopics"}, Tests: archtest.WithTests},
			Check: archtest.AllowImports(),
			Hint:  "help topics are plain text shared by every surface",
		},
		{
			Name:  "credentials-import-nothing-of-the-module",
			Files: archtest.Files{Under: []string{"internal/credentials"}, Tests: archtest.WithTests},
			Check: archtest.ForbidImports("internal/service", "internal/client", "internal/cli", "internal/control"),
			Hint:  "the store sits below the service and the command surface and knows neither",
		},
		{
			Name:  "stacks-read-the-working-copy-only",
			Files: archtest.Files{Under: []string{"internal/stacks"}, Tests: archtest.WithTests},
			Check: archtest.ForbidImports("internal/service", "internal/client", "internal/cli", "internal/control"),
			Hint:  "reading stacks is pure and sits below the service and the command surface",
		},
		{
			Name:  "hostkey-knows-no-service-layer",
			Files: archtest.Files{Under: []string{"internal/hostkey"}, Tests: archtest.WithTests},
			Check: archtest.ForbidImports("internal/service", "internal/client", "internal/cli", "internal/control"),
			Hint:  "host key handling sits below the service and the command surface and knows neither",
		},
		{
			Name:  "gitrepo-runs-git-only",
			Files: archtest.Files{Under: []string{"internal/gitrepo"}, Tests: archtest.WithTests},
			Check: archtest.ForbidImports("internal/service", "internal/client", "internal/cli", "internal/control"),
			Hint:  "the git layer sits below the service and the command surface and knows neither",
		},
	}, nil)
}
