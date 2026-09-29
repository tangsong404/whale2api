#!/bin/sh
# Entry point for the device-harvest sidecar.
#
# Headed Chromium needs an X display, so every command runs under xvfb-run.
# xvfb-run must NOT be PID 1: Xvfb refuses to send its readiness SIGUSR1 to a
# PID-1 parent (that would terminate the container init), so xvfb-run as PID 1
# hangs forever waiting for it. Keeping this shell as PID 1 and running
# xvfb-run as a session child keeps the readiness handshake working.
set -e

setsid xvfb-run -a "$@" &
child=$!

shutdown() {
    trap - TERM INT
    # Signal the whole session (xvfb-run + node + Xvfb), then wait briefly.
    kill -TERM -- "-$child" 2>/dev/null || kill -TERM "$child" 2>/dev/null || true
    for _ in 1 2 3 4 5; do
        kill -0 "$child" 2>/dev/null || break
        sleep 1
    done
    kill -KILL -- "-$child" 2>/dev/null || true
    exit 0
}
trap shutdown TERM INT

wait "$child"
