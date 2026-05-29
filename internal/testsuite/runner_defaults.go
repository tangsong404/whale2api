package testsuite

import "time"

func DefaultOptions() Options {
	return Options{
		ConfigPath:  "tests/fixtures/gateway-pool.json",
		OutputDir:   "artifacts/testsuite",
		Port:        0,
		Timeout:     120 * time.Second,
		Retries:     2,
		NoPreflight: false,
		MaxKeepRuns: 5,
	}
}
