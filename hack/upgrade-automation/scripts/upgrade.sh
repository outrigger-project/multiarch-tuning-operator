#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$REPO_ROOT"

export GOTOOLCHAIN=local
GO_BINARY=go
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
  case "${args[i]}" in
    --go-bin|-go-bin)
      if ((i + 1 >= ${#args[@]})); then
        echo "--go-bin requires a compiler executable" >&2
        exit 1
      fi
      GO_BINARY="${args[++i]}"
      ;;
    --go-bin=*|-go-bin=*) GO_BINARY="${args[i]#*=}" ;;
    --mode|-mode|--go-target|-go-target|--k8s-target|-k8s-target|--report|-report) i=$((i + 1)) ;;
    --|[!-]*) break ;;
  esac
done
exec "$GO_BINARY" run ./hack/upgrade-automation/cmd/updater "$@"
