#!/bin/sh

set -eux

#cd /tmp
GOFLAGS='' go install github.com/securego/gosec/v2/cmd/gosec@latest
gosec -severity medium -confidence medium "${@}"
#cd -
