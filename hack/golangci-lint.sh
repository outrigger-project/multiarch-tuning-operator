#!/bin/sh

set -euxo pipefail
echo "Running golangci-lint..."

if [ "${#}" -gt 0 ]; then
  pushd "${1}"
  trap 'popd || true' ERR EXIT SIGINT SIGTERM
fi

GOFLAGS='' go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLINT_VERSION:-latest}"
golangci-lint run --verbose ./...
