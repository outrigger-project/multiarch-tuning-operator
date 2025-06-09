#!/bin/sh

set -eux
echo "Running golangci-lint..."
GOFLAGS='' go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLINT_VERSION:-latest}"
golangci-lint run --verbose

pushd enoexec-daemon
golangci-lint run --verbose
popd