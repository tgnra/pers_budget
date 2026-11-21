#!/bin/sh
# Builds ./budget.
# Cross-compile with e.g. GOOS=linux GOARCH=arm64 ./build.sh
set -e
cd "$(dirname "$0")"
go build -o budget .
echo "Built ./budget"
