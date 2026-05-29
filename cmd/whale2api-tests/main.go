package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"whale2api/internal/testsuite"
)

func main() {
	opts := testsuite.DefaultOptions()
	var timeoutSeconds int

	flag.StringVar(&opts.ConfigPath, "pool-fixture", opts.ConfigPath, "Gateway pool fixture JSON (keys + accounts) seeded into SQLite before the server starts")
	flag.StringVar(&opts.OutputDir, "out", opts.OutputDir, "Output artifact directory")
	flag.IntVar(&opts.Port, "port", opts.Port, "Server port (0 means auto-select free port)")
	flag.IntVar(&timeoutSeconds, "timeout", int(opts.Timeout.Seconds()), "Per-request timeout in seconds")
	flag.IntVar(&opts.Retries, "retries", opts.Retries, "Retry count for network/5xx requests")
	flag.BoolVar(&opts.NoPreflight, "no-preflight", opts.NoPreflight, "Skip preflight checks")
	flag.IntVar(&opts.MaxKeepRuns, "keep", opts.MaxKeepRuns, "Max test runs to keep (0 = keep all)")
	flag.Parse()

	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}
	opts.Timeout = time.Duration(timeoutSeconds) * time.Second

	if err := testsuite.Run(context.Background(), opts); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	_, _ = fmt.Fprintln(os.Stdout, "testsuite completed successfully")
}
