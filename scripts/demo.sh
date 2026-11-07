#!/usr/bin/env bash
# Runs the end-to-end demo against an API (default: local docker compose).
#   bash scripts/demo.sh                      # http://localhost:8080
#   bash scripts/demo.sh https://api.example.com
set -euo pipefail
cd "$(dirname "$0")/.."
go run ./scripts/demo -base "${1:-http://localhost:8080}"
