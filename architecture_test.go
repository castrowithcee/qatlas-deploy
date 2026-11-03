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
			Name:  "helptopics-import-nothing-of-the-module",
			Files: archtest.Files{Under: []string{"internal/helptopics"}, Tests: archtest.WithTests},
			Check: archtest.AllowImports(),
			Hint:  "help topics are plain text shared by every surface",
		},
	}, nil)
}
