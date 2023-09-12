#!/bin/sh

set -eux
echo "Running golangci-lint..."
GOFLAGS='' go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.54.2
golangci-lint run --timeout 5m0s --verbose --skip-dirs vendor --skip-files zz_generated*
