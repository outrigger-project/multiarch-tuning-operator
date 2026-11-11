# Dependency updater sync

Approved scope: sync dependency automation starting with commit 55a0f983980abb94d4d61dca4fb660356afec420 through 06dd1aadd2c06796fb9a30ce8306223e679cb67a from the sibling operator checkout.

## Goals and constraints
Bring Kubernetes release targeting, launcher fixes, report preservation, rollback hardening, lint cleanup and regression coverage into `hack/upgrade-automation`. Preserve module identity `github.com/openshift/multiarch-tuning-operator`. Include no source-operator-specific references, even in negative fixtures; use an unrelated example module instead. Do not perform dependency upgrades or change root module/vendor contents.

## Approaches
Use an adapted directory sync: upstream already contains the complete tested implementation. Raw copying would import incorrect identity; cherry-picking the original addition collides with existing files. Keep compiled local test artifacts out of the port.

## Verification
Run regression tests before and after port, full updater tests, vet, build, launcher syntax/help, reference scan, and whitespace check. Attempt repository-wide non-e2e tests; report environment blockers rather than changing dependencies.
