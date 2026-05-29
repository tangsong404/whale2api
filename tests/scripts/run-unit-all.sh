#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT_DIR"

bash ./tests/scripts/run-unit-go.sh
bash ./tests/scripts/run-unit-node.sh
