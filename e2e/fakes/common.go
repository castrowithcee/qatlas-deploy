//go:build e2efakes

package main

import (
	"net/http"
	"os"
)

const (
	certFile = "/certs/server.crt"
	keyFile  = "/certs/server.key"
)

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// serveTLS listens on addr with the shared test certificate.
func serveTLS(addr string, h http.Handler) error {
	return (&http.Server{Addr: addr, Handler: h}).ListenAndServeTLS(certFile, keyFile)
}
