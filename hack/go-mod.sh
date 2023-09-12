#!/bin/sh

set -eux

go mod tidy
go mod vendor
go mod verify
