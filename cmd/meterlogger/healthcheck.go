package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

const healthcheckTimeout = 5 * time.Second

//nolint:gochecknoglobals // cobra CLI pattern requires package-level variables
var healthcheckCmd = &cobra.Command{
	Use:   "healthcheck",
	Short: "Probe the local meterlogger /readyz endpoint",
	Long: "Probe the local meterlogger /readyz endpoint and exit 0 on HTTP 200, 1 otherwise. " +
		"Designed for use in Docker HEALTHCHECK directives where curl is not available. " +
		"The HTTP port is read from HTTPSERVER_PORT (default: 8080).",
	Run: func(_ *cobra.Command, _ []string) {
		os.Exit(runHealthcheck())
	},
}

//nolint:gochecknoinits // init() is required by the cobra CLI pattern
func init() {
	rootCmd.AddCommand(healthcheckCmd)
}

const healthcheckDefaultPort = 8080

func runHealthcheck() int {
	port := healthcheckDefaultPort
	if v := os.Getenv("HTTPSERVER_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			port = p
		}
	}

	client := &http.Client{Timeout: healthcheckTimeout}
	url := fmt.Sprintf("http://localhost:%d/readyz", port)
	//nolint:gosec,noctx // G107/G704: localhost-only probe; timeout enforced via client
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}
