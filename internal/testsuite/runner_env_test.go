package testsuite

import (
	"reflect"
	"testing"
)

func TestPreflightStepsExactSequence(t *testing.T) {
	want := [][]string{
		{"go", "test", "./...", "-count=1"},
		{"./tests/scripts/check-node-split-syntax.sh"},
		{"node", "--test", "tests/node/stream-tool-sieve.test.js", "tests/node/chat-stream.test.js", "tests/node/js_compat_test.js"},
	}

	got := preflightSteps()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("preflight steps mismatch\nwant=%v\ngot=%v", want, got)
	}
}
