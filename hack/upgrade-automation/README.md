# Upgrade Automation for Multiarch Tuning Operator

This directory contains automated scripts for upgrading Go and Kubernetes versions in the multiarch-tuning-operator.

## Philosophy

**Dynamic Discovery over Hardcoded Mappings**: The upgrade scripts automatically:

1. **Discover versions dynamically** - Find compatible versions from authoritative sources (no hardcoded version tables)
2. **Update files consistently** - Apply changes to all required files following established patterns
3. **Execute complete workflow** - Run all upgrade steps in the correct order with validation

This approach ensures:
- ✅ **Single source of truth** - The operator team owns and maintains the upgrade process
- ✅ **No hardcoded versions** - All versions discovered from GitHub releases, go.mod files, OCP mirrors
- ✅ **Reproducible** - Same inputs always produce the same results
- ✅ **Version-controlled** - Changes to the process are tracked with the code

## Quick Start
```bash
cd /path/to/multiarch-tuning-operator

# Let the script discover Go and K8s versions from OCP 4.20
hack/upgrade-automation/scripts/upgrade.sh 4.20

# Specify Go version, discover K8s from OCP 4.21
hack/upgrade-automation/scripts/upgrade.sh 4.21 1.24

# Specify all versions explicitly
hack/upgrade-automation/scripts/upgrade.sh 4.21 1.24 1.34.1
```

The script will:
1. ✅ Validate prerequisites and create upgrade branch
2. ✅ Discover all compatible tool versions
3. ✅ Update all required files
4. ✅ Run code generation and tests
5. ✅ Create 6-7 structured commits

## Upgrade Workflow

The script executes 6 sequential steps, creating a git commit for each:

### Step 1: Update Go version in base images

Updates all container image references with Go minor version (e.g., 1.24.11 → 1.24):
- `.ci-operator.yaml` - CI builder: `rhel-9-golang-{go}-openshift-{ocp}`
- `.tekton/*.yaml` - Konflux pipelines: `rhel_9_{go}`
- `Dockerfile` - Base image: `FROM golang:{go}`
- `Makefile` - BUILD_IMAGE variable
- `bundle.konflux.Dockerfile`, `konflux.Dockerfile` - Konflux builders

**Commit:** `Update Makefile and Dockerfiles to use the new Golang version base image to {go_minor}`

### Step 2: Update tools in Makefile

Discovers and updates tool versions compatible with target Go version:
- `KUSTOMIZE_VERSION` - v5.x series
- `CONTROLLER_TOOLS_VERSION` - Matches K8s minor version
- `SETUP_ENVTEST_VERSION` - Derived from controller-runtime
- `ENVTEST_K8S_VERSION` - From controller-runtime's dependencies
- `GOLINT_VERSION` - Latest compatible

**Commit:** `Update tools in Makefile`

### Step 3: Update go.mod

Updates all Go dependencies (smart handling skips incompatible versions):
1. Sets go directive (full patch version from BUILD_IMAGE container)
2. Updates `k8s.io/*` → v{k8s}
3. Updates `sigs.k8s.io/controller-runtime` → compatible version
4. Updates `github.com/openshift/*` → release-{ocp}
5. Updates all other dependencies with `go get -u` (latest compatible)
6. Runs `go mod tidy`, verifies, consolidates to 2 require blocks

**Commit:** `pin K8S API to v{k8s_version} and set go minimum version to {go_version}`

### Step 4: Update vendor

Re-vendors all dependencies:
1. `go mod tidy`
2. `rm -rf vendor/`
3. `go mod vendor`
4. Restores go directive if `go mod tidy` upgraded it

**Commit:** `go mod vendor`

### Step 5: Run code generation

Regenerates code (only commits if changes occur):
- `make generate` - Deepcopy implementations
- `make manifests` - CRDs and RBAC
- `make bundle` - OLM bundle

**Commit (if changes):** `Code generation after upgrade`

### Step 6: Run tests and build

Validates upgrade (commits fixes if needed):
- `make docker-build`
- `make build`
- `make test`

**Commit (if changes):** `Update code after Golang, pivot to k8s {k8s_version} and dependencies upgrade`

### Expected Result

```bash
$ git log --oneline HEAD~7..HEAD
a1b2c3d Update code after Golang, pivot to k8s 1.34.1 and dependencies upgrade  # Step 6 (if needed)
d4e5f6g Code generation after upgrade                                          # Step 5 (if needed)
h7i8j9k go mod vendor                                                          # Step 4
l0m1n2o pin K8S API to v1.34.1 and set go minimum version to 1.24.11          # Step 3
p3q4r5s Update tools in Makefile                                               # Step 2
t6u7v8w Update Makefile and Dockerfiles to use the new Golang version...      # Step 1
```

## Version Discovery

The script queries authoritative sources (no hardcoded mappings):

| Component | Source | Example |
|-----------|--------|---------|
| **Kubernetes** | OCP release mirrors or openshift/api | `https://mirror.openshift.com/.../release.txt`<br>`https://raw.githubusercontent.com/openshift/api/release-{ocp}/go.mod` |
| **Go** | openshift/api + go.dev | `https://raw.githubusercontent.com/openshift/api/release-{ocp}/go.mod`<br>`https://go.dev/dl/?mode=json` |
| **controller-runtime** | GitHub releases API | `https://api.github.com/repos/kubernetes-sigs/controller-runtime/releases`<br>Matches K8s minor + Go compatibility |
| **kustomize** | GitHub releases API | `https://api.github.com/repos/kubernetes-sigs/kustomize/releases`<br>Filters by Go compatibility |
| **controller-tools** | GitHub releases API | `https://api.github.com/repos/kubernetes-sigs/controller-tools/releases`<br>Matches K8s minor |
| **golangci-lint** | GitHub releases API | `https://api.github.com/repos/golangci/golangci-lint/releases`<br>Filters by Go compatibility |

**Discovery algorithm for tools:**
```bash
for each release in GitHub API:
    fetch go.mod from release
    if go_required <= target_go:
        return release version
fallback to current Makefile version
```

## Script Architecture

### Main Orchestrator: `scripts/upgrade.sh` (725 lines)

**Structure:**
```bash
main()
├── validate_in_operator_repo()
├── discover_k8s_from_ocp_release()
├── discover_latest_go_patch()
├── validate_prerequisites()
├── discover_controller_runtime_version()
├── discover tool versions (5 tools)
├── step_update_base_images()
├── step_update_tools()
├── step_update_go_mod()
├── step_update_vendor()
├── step_run_code_generation()
└── step_run_tests()
```

**Each step function:**
1. Prints progress header
2. Calls lib functions to do work
3. Creates git commit
4. Reports completion

### Library: `scripts/lib/version-discovery.sh` (313 lines)

**Discovery functions:**

| Function | Source | Returns |
|----------|--------|---------|
| `discover_k8s_from_ocp_release()` | OCP release mirrors | K8s version (e.g., 1.34.1) |
| `discover_latest_go_patch()` | go.dev/dl/?mode=json | Go version (e.g., 1.24.11) |
| `discover_current_ocp_version()` | Makefile BUILD_IMAGE | OCP version (e.g., 4.19) |
| `discover_controller_runtime_version()` | GitHub releases API | controller-runtime version |
| `discover_kustomize_version()` | GitHub releases API | kustomize version |
| `discover_controller_tools_version()` | GitHub releases API | controller-tools version |
| `discover_golangci_lint_version()` | GitHub releases API | golangci-lint version |
| `derive_setup_envtest_version()` | controller-runtime version | setup-envtest version |
| `extract_envtest_k8s_version()` | controller-runtime go.mod | envtest K8s version |

### Library: `scripts/lib/file-updates.sh` (147 lines)

**Portable sed function:**
```bash
sed_inplace() {
    if sed --version >/dev/null 2>&1; then
        sed -i "$pattern" "$file"  # GNU sed (Linux)
    else
        sed -i '' "$pattern" "$file"  # BSD sed (macOS)
    fi
}
```

**Update functions:**

| Function | File(s) Updated | Pattern |
|----------|-----------------|---------|
| `update_ci_operator_yaml()` | .ci-operator.yaml | rhel-9-golang-X.Y-openshift-X.Y |
| `update_tekton_files()` | .tekton/*.yaml | rhel_9_X.Y |
| `update_dockerfile()` | Dockerfile | FROM golang:X.Y |
| `update_makefile_build_image()` | Makefile | BUILD_IMAGE |
| `update_bundle_konflux_dockerfile()` | bundle.konflux.Dockerfile | rhel_9_X.Y |
| `update_konflux_dockerfile()` | konflux.Dockerfile | rhel_9_X.Y |
| `update_go_mod_directive()` | go.mod | go X.Y |
| `update_makefile_tool_versions()` | Makefile | 5 tool variables |

### Library: `scripts/lib/validations.sh` (130 lines)

**Validation functions:**

| Function | Checks | Action on Failure |
|----------|--------|-------------------|
| `validate_clean_working_directory()` | No uncommitted changes | Exit with error |
| `validate_golang_image_exists()` | golang:{version} exists | Exit with error |
| `validate_hostmount_scc_function()` | SCC mapping up-to-date | Warn for manual review |
| `validate_k8s_version_consistency()` | All k8s.io/* same minor | Warn if inconsistent |
| `validate_and_create_branch()` | Create upgrade branch | Prompt to recreate if exists |
| `validate_prerequisites()` | All above checks | Exit on any failure |

**Branch naming:**
```
upgrade-ocp-{ocp}-go-{go_minor}-k8s-{k8s_minor}

Example: upgrade-ocp-4.20-go-1.24-k8s-1.34
```

## Troubleshooting

### Error: "Working directory is not clean"

**Cause:** Uncommitted changes in git working directory

**Solution:**
```bash
# Commit changes
git add .
git commit -m "Your changes"

# Or stash changes
git stash

# Then run upgrade
hack/upgrade-automation/scripts/upgrade.sh 4.20
```

### Error: "Could not discover K8s version for OCP X.Y"

**Cause:** OCP version not yet released or mirrors unavailable

**Solution:** Specify K8s version manually:
```bash
# Find K8s version from openshift/api
curl -s https://raw.githubusercontent.com/openshift/api/release-4.20/go.mod | grep k8s.io/api

# Then specify it
hack/upgrade-automation/scripts/upgrade.sh 4.20 1.24 1.34.1
```

### Error: "go mod tidy changed go directive from X to Y"

**Cause:** Some dependencies require newer Go version than target

**Meaning:** K8s/OCP versions you specified need Go Y, but you're targeting Go X

**Solutions:**
1. Use newer Go version:
   ```bash
   hack/upgrade-automation/scripts/upgrade.sh 4.20 1.25
   ```

2. Or use older K8s/OCP versions compatible with your Go version

### Error: "make test failed"

**Common causes and fixes:**

**API deprecation:**
```
Error: undefined: corev1.SomeOldAPI
```
Solution: Update code to use new API (check K8s release notes)

**Test helper changes:**
```
Error: cannot use X (type Y) as type Z
```
Solution: Update test setup code for new types

**Import path changes:**
```
Error: package X is not in GOROOT
```
Solution: Update import paths (check go.mod for correct versions)

### Error: "make docker-build failed"

**Cause:** Container image doesn't exist or build errors

**Check image exists:**
```bash
# For golang base image
docker manifest inspect golang:1.24

# For BUILD_IMAGE
skopeo inspect docker://registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.24-openshift-4.20
```

**Build errors:** Review Dockerfile changes and ensure all dependencies available

### Warning: "Using current version from Makefile"

**Cause:** Couldn't find compatible tool version from GitHub

**Impact:** Tool won't be updated, will keep current version

**Action:** Usually safe, but verify manually if needed:
```bash
# Check current version
grep KUSTOMIZE_VERSION Makefile

# Check latest compatible
curl -s https://api.github.com/repos/kubernetes-sigs/kustomize/releases | grep tag_name
```

### Error: "Not in an operator repository"

**Cause:** Running script from wrong directory

**Solution:**
```bash
cd /path/to/multiarch-tuning-operator
hack/upgrade-automation/scripts/upgrade.sh 4.20
```

## Post-Upgrade Actions

After the script completes successfully, you need to:

### 1. Review the Changes

```bash
# View all commits
git log --oneline HEAD~7..HEAD

# Review each commit
git show HEAD~6  # Step 1: Base images
git show HEAD~5  # Step 2: Tools
git show HEAD~4  # Step 3: go.mod
git show HEAD~3  # Step 4: vendor
git show HEAD~2  # Step 5: Code generation (if exists)
git show HEAD~1  # Step 6: Test fixes (if exists)
```

### 2. Check for Prow Config Updates (if needed)

The PR in this repo may need a companion PR in openshift/release:

**Example:** https://github.com/openshift/release/pull/55728/commits/707fa080a66d8006c4a69e452a4621ed54f67cf6

**Check if needed:**
```bash
# Look for CI job golang version references
grep -r "golang.*1\\.22" openshift/release/ci-operator/config/openshift/multiarch-tuning-operator/
```

### 3. Update Documentation

If you discovered any process changes, update:
- `docs/ocp-release.md` - Manual upgrade procedure
- This README if script behavior changed

### 4. Create Pull Request

```bash
# Push upgrade branch
git push -u origin $(git branch --show-current)

# Create PR (via gh CLI or GitHub UI)
gh pr create --title "Upgrade to OCP 4.20, Go 1.24, K8s 1.34" \
             --body "Automated upgrade using hack/upgrade-automation/scripts/upgrade.sh"
```

## Platform Compatibility

### Linux (GNU sed)
✅ Fully supported - default platform

### macOS (BSD sed)
✅ Fully supported via `sed_inplace()` helper

**How it works:**
```bash
# Detects sed flavor by testing --version flag
if sed --version >/dev/null 2>&1; then
    # GNU sed
    sed -i "pattern" file
else
    # BSD sed (requires empty string for extension)
    sed -i '' "pattern" file
fi
```

## When to Update the Scripts

### New file needs updating

**Add to `scripts/lib/file-updates.sh`:**
```bash
update_new_file() {
    local version="$1"
    sed_inplace "s/old_pattern/${version}/" path/to/file
    echo "  ✅ Updated path/to/file" >&2
}
```

**Call from `scripts/upgrade.sh` in appropriate step**

### New tool needs version management

**1. Add discovery in `scripts/lib/version-discovery.sh`:**
```bash
discover_new_tool_version() {
    local go_version="$1"
    local go_minor
    go_minor=$(echo "$go_version" | cut -d. -f2)

    local releases
    releases=$(curl -s https://api.github.com/repos/org/tool/releases |
               grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')

    for version in $releases; do
        local go_req
        go_req=$(curl -sf "https://raw.githubusercontent.com/org/tool/v${version}/go.mod" |
                 grep '^go ' | awk '{print $2}' | cut -d. -f2)

        if [[ -n "$go_req" ]] && [[ "$go_req" -le "$go_minor" ]]; then
            echo "✅ Found tool v$version (requires Go 1.$go_req)" >&2
            echo "$version"
            return 0
        fi
    done

    # Fallback to current
    local current
    current=$(grep 'TOOL_VERSION' Makefile | grep -oP 'v\K[0-9]+\.[0-9]+\.[0-9]+')
    echo "⚠️  Using current: v$current" >&2
    echo "$current"
}
```

**2. Add update in `scripts/lib/file-updates.sh`:**
```bash
update_makefile_tool_versions() {
    # ... existing updates ...
    sed_inplace "s/\(TOOL_VERSION\s*=\s*\)v[0-9]\+\.[0-9]\+\.[0-9]\+/\1${tool_version}/" Makefile
}
```

**3. Call from `scripts/upgrade.sh` step 2**

### Upgrade workflow changes

**Update `scripts/upgrade.sh`:**
- Modify step functions to change behavior
- Add new steps if needed
- Update commit messages

**Update this README:**
- Document new steps
- Update "Expected Commit History"
- Update "Files Updated" section

## Design Philosophy

### Why Dynamic Discovery?

**Problem:** Hardcoded version mappings become outdated

**Bad approach:**
```bash
# ❌ Becomes stale
case $ocp_version in
    4.19) k8s_version="1.32.3" ;;
    4.20) k8s_version="1.34.1" ;;
    # Needs manual update for each release
esac
```

**Good approach:**
```bash
# ✅ Always current
k8s_version=$(curl -sf "https://mirror.openshift.com/.../release.txt" |
              grep 'kubernetes ' | awk '{print $2}')
```

### Why Consolidated Scripts?

**Before:** 10 separate script files
**After:** 4 files (1 main + 3 libraries)

**Benefits:**
- Easier to follow complete workflow
- Less duplication
- Simpler maintenance
- Clear separation: orchestration (upgrade.sh) vs utilities (lib/)

### Why Structured Commits?

Each step gets its own commit because:
- **Reviewable** - Each commit is focused and easy to review
- **Revertible** - Can revert individual steps if needed
- **Traceable** - Git history shows what changed when
- **Documented** - Commit messages explain why

## See Also

- [../../docs/ocp-release.md](../../docs/ocp-release.md) - Manual upgrade documentation
- [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html) - Operator development
- [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime) - Controller framework