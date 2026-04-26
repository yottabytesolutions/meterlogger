// Command healthcheck performs a readiness probe against the local meterlogger instance.
// It exits 0 if /readyz returns HTTP 200, and exits 1 otherwise.
//
// Designed for use in Docker HEALTHCHECK directives where curl is not available (scratch image).
// The HTTP port is read from the HTTPSERVER_PORT environment variable (default: 8080).
package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
)

func main() {
	port := 8080
	if v := os.Getenv("HTTPSERVER_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			port = p
		}
	}

	url := fmt.Sprintf("http://localhost:%d/readyz", port)
	resp, err := http.Get(url) //nolint:gosec,noctx // G107: localhost-only probe, no context needed
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		os.Exit(1)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: status %d\n", resp.StatusCode)
		os.Exit(1)
	}
}
