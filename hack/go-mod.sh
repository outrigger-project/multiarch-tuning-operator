#!/bin/sh

set -eux
go mod tidy
go mod vendor
go mod verify

pushd enoexec-daemon
go mod tidy
go mod vendor
go mod verify
popd

