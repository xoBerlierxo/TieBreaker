#!/usr/bin/env bash
# Runs the end-to-end demo against an API (default: local docker compose).
#   bash scripts/demo.sh                              # http://localhost:8080, sample cars
#   bash scripts/demo.sh https://api.example.com      # another API
#   bash scripts/demo.sh http://localhost:8080 nexon  # shortlist cars matching "nexon"
set -euo pipefail
cd "$(dirname "$0")/.."
go run ./scripts/demo -base "${1:-http://localhost:8080}" -q "${2:-sample}"
