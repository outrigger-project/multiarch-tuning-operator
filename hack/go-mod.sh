#!/bin/sh
set -euxo pipefail

if [ "${#}" -gt 0 ]; then
  pushd "${1}"
  trap 'popd || true' ERR EXIT SIGINT SIGTERM
fi

go mod tidy
go mod vendor

# TODO: Remove once openshift/library-go implements HasSyncedChecker for fakeSharedIndexInformer (k8s.io/client-go v0.36)
rm -f vendor/github.com/openshift/library-go/pkg/operator/v1helpers/test_helpers.go

go mod verify
