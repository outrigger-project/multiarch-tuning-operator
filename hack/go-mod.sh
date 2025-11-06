#!/bin/sh
set -euxo pipefail

if [ "${#}" -gt 0 ]; then
  pushd "${1}"
  trap 'popd || true' ERR EXIT SIGINT SIGTERM
fi

go mod tidy
go mod vendor
go mod verify
