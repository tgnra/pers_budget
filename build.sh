#!/bin/sh
# Hashes accounts.txt into accounts_gen.go, then builds ./budget.
# Cross-compile with e.g. GOOS=linux GOARCH=arm64 ./build.sh
set -e
cd "$(dirname "$0")"
env -u GOOS -u GOARCH go generate  # the generator runs on this machine
go build -o budget .
echo "Built ./budget"
