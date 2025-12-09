#!/bin/bash
# Main upgrade orchestrator for multiarch-tuning-operator
#
# This script orchestrates the complete upgrade process by:
# 1. Validating prerequisites
# 2. Discovering compatible versions
# 3. Running each upgrade step in sequence
# 4. Providing post-upgrade guidance
#
# IMPORTANT: This script must be run from the root of the operator repository

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/version-discovery.sh"
source "$SCRIPT_DIR/lib/validations.sh"
source "$SCRIPT_DIR/lib/file-updates.sh"

usage() {
    cat <<EOF
Usage: $0 <ocp-version> [go-version] [k8s-version]

Arguments:
  ocp-version   Target OCP version (e.g., 4.19, 4.20, 4.21)
  go-version    Target Go version (optional, will be discovered if not specified)
  k8s-version   Target Kubernetes version (optional, will be discovered from OCP if not specified)

Example:
  cd /path/to/multiarch-tuning-operator
  $0 4.20                    # Discover Go and K8s from OCP 4.20
  $0 4.21 1.24               # Discover K8s from OCP 4.21, use Go 1.24
  $0 4.21 1.24 1.34.1        # Specify all versions explicitly

This script will:
  1. Validate prerequisites and version compatibility
  2. Discover compatible versions for all dependencies and tools
  3. Update base images, go.mod, vendor, and tools
  4. Run code generation and tests
  5. Create structured commits
  6. Provide post-upgrade guidance

IMPORTANT:
  - This script must be run from the root of the multiarch-tuning-operator repository
  - It will modify files in the current directory
EOF
}

validate_in_operator_repo() {
    local expected_module="github.com/openshift/multiarch-tuning-operator"

    echo "Validating we're in an operator repository..." >&2

    # Check for key files that identify this as an operator repo
    local required_files=(
        "go.mod"
        "Makefile"
    )

    for file in "${required_files[@]}"; do
        if [[ ! -f "$file" ]]; then
            echo "❌ Error: Not in an operator repository" >&2
            echo "   Missing required file: $file" >&2
            echo "   Current directory: $(pwd)" >&2
            echo "" >&2
            echo "Please cd to the operator repository root and try again." >&2
            return 1
        fi
    done

    # Verify this is the multiarch-tuning-operator by checking go.mod module name
    local actual_module
    actual_module=$(grep '^module' go.mod 2>/dev/null | awk '{print $2}')

    if [[ "$actual_module" != "$expected_module" ]]; then
        echo "❌ Error: This doesn't appear to be the multiarch-tuning-operator repository" >&2
        echo "   Expected module: $expected_module" >&2
        echo "   Found module: $actual_module" >&2
        echo "   Current directory: $(pwd)" >&2
        return 1
    fi

    local operator_name
    operator_name=$(echo "$actual_module" | sed 's/.*\///')

    echo "✅ Confirmed: Running in $operator_name repository" >&2
    echo "   Module: $actual_module" >&2
    echo "   Path: $(pwd)" >&2
    echo "" >&2
    return 0
}

# ==============================================================================
# Step 1: Update Go version in base images
# ==============================================================================
step_update_base_images() {
    local go_version="$1"
    local k8s_version="$2"
    local ocp_version="$3"

    # Extract minor version for Docker images (e.g., 1.24.11 -> 1.24)
    local go_minor
    go_minor=$(echo "$go_version" | cut -d. -f1-2)

    echo "======================================" >&2
    echo "Step 1: Update Go version in base images" >&2
    echo "======================================" >&2
    echo "" >&2

    # Update all base image files (use minor version for images)
    update_ci_operator_yaml "$go_minor" "$ocp_version"
    update_tekton_files "$go_minor"
    update_dockerfile "$go_minor"
    update_makefile_build_image "$go_minor" "$ocp_version"
    update_bundle_konflux_dockerfile "$go_minor"
    update_konflux_dockerfile "$go_minor"

    echo "" >&2

    # Validate operator-specific function
    validate_hostmount_scc_function "$k8s_version" "$ocp_version"

    echo "" >&2

    # Commit changes
    echo "Committing changes..." >&2
    git add .ci-operator.yaml .tekton/ Dockerfile Makefile bundle.konflux.Dockerfile konflux.Dockerfile 2>/dev/null || true
    git commit -m "Update Makefile and Dockerfiles to use the new Golang version base image to ${go_minor}"

    echo "✅ Step 1 complete" >&2
}

# ==============================================================================
# Step 2: Update tools in Makefile
# ==============================================================================
step_update_tools() {
    local kustomize_version="$1"
    local controller_tools_version="$2"
    local setup_envtest_version="$3"
    local envtest_k8s_version="$4"
    local golint_version="$5"

    echo "======================================" >&2
    echo "Step 2: Update tools in Makefile" >&2
    echo "======================================" >&2
    echo "" >&2

    update_makefile_tool_versions \
        "$kustomize_version" \
        "$controller_tools_version" \
        "$setup_envtest_version" \
        "$envtest_k8s_version" \
        "$golint_version"

    echo "" >&2
    echo "Committing changes..." >&2
    git add Makefile
    git commit -m "Update tools in Makefile"

    echo "✅ Step 2 complete" >&2
}

# ==============================================================================
# Helper: Find latest compatible patch version for a dependency
# ==============================================================================
try_update_to_latest_compatible() {
    local dep="$1"
    local go_version="$2"
    local current_version

    # Get current version
    current_version=$(go list -m "$dep" 2>/dev/null | awk '{print $2}')
    if [[ -z "$current_version" ]]; then
        return 1
    fi

    # Extract major.minor from current version (e.g., v0.34.2 -> 0.34)
    local major_minor
    major_minor=$(echo "$current_version" | sed -E 's/^v?([0-9]+\.[0-9]+)\..*/\1/')

    # Get all versions for this dependency
    local all_versions
    all_versions=$(go list -mod=mod -m -versions "$dep" 2>/dev/null)
    if [[ -z "$all_versions" ]]; then
        return 1
    fi

    # Filter to same major.minor, stable releases only (no -alpha, -beta, -rc)
    # Sort in reverse version order (newest first)
    local patch_versions
    patch_versions=$(echo "$all_versions" | tr ' ' '\n' | \
                     grep -v "^$dep$" | \
                     grep -E "^v?${major_minor}\.[0-9]+$" | \
                     sort -V -r)

    if [[ -z "$patch_versions" ]]; then
        return 1
    fi

    # Try each version from newest to oldest
    local found_update=0
    for version in $patch_versions; do
        # Skip if it's the current version or older
        if [[ "$version" == "$current_version" ]]; then
            continue
        fi

        # Compare versions (skip if older)
        if printf '%s\n%s\n' "$version" "$current_version" | sort -V -C 2>/dev/null; then
            # version <= current_version, skip
            continue
        fi

        # Try to update
        if go get "${dep}@${version}" >/dev/null 2>&1; then
            # Check if go mod tidy works
            if go mod tidy >/dev/null 2>&1; then
                # Check if go version didn't change
                local new_go_version
                new_go_version=$(grep '^go ' go.mod | awk '{print $2}')

                if [[ "$new_go_version" == "$go_version" ]]; then
                    echo "    ✅ Updated to $version (latest compatible patch)" >&2
                    found_update=1
                    break
                fi
            fi
        fi
    done

    if [[ $found_update -eq 1 ]]; then
        return 0
    else
        return 1
    fi
}

# ==============================================================================
# Step 3: Update go.mod
# ==============================================================================
step_update_go_mod() {
    local k8s_version="$1"
    local ocp_version="$2"
    local controller_runtime_version="$3"
    local go_version="$4"

    echo "======================================" >&2
    echo "Step 3: Update go.mod" >&2
    echo "======================================" >&2
    echo "" >&2

    echo "Using Go version: $go_version in go.mod" >&2
    echo "" >&2

    # Set GOTOOLCHAIN to enforce the exact Go version
    # Using go1.24.6 format prevents Go from upgrading to newer versions
    export GOTOOLCHAIN="go${go_version}"
    echo "Setting GOTOOLCHAIN=go${go_version} to enforce version constraint" >&2
    echo "" >&2

    # Update go directive
    update_go_mod_directive "$go_version"

    echo "" >&2

    # Update k8s.io dependencies
    # Convert k8s version from "1.34.1" to "0.34.1" for k8s.io modules
    local k8s_module_version
    k8s_module_version=$(echo "$k8s_version" | sed 's/^1\./0./')
    echo "Updating k8s.io dependencies to v${k8s_module_version}..." >&2
    local k8s_deps
    k8s_deps=$(grep -E '^\s+k8s\.io/' go.mod | grep -v '//' | awk '{print $1}' | sort -u)

    for dep in $k8s_deps; do
        echo "  Updating $dep..." >&2
        if go get "${dep}@v${k8s_module_version}" 2>&1; then
            echo "    ✅ Updated to v${k8s_module_version}" >&2
        else
            echo "    ⚠️  v${k8s_module_version} not available, using compatible version" >&2
        fi
    done

    echo "" >&2

    # Update controller-runtime
    echo "Updating controller-runtime to v${controller_runtime_version}..." >&2
    go get "sigs.k8s.io/controller-runtime@v${controller_runtime_version}"
    echo "  ✅ Updated controller-runtime" >&2

    echo "" >&2

    # Update OpenShift dependencies if present
    if grep -q 'github.com/openshift' go.mod; then
        echo "Updating OpenShift dependencies to release-${ocp_version}..." >&2
        local openshift_deps
        openshift_deps=$(grep 'github.com/openshift' go.mod | grep -v '//' | grep -v '^module ' | awk '{print $1}' | sort -u)

        for dep in $openshift_deps; do
            echo "  Updating $dep..." >&2
            if go get "${dep}@release-${ocp_version}" 2>&1; then
                echo "    ✅ Updated to release-${ocp_version}" >&2
            else
                echo "    ⚠️  release-${ocp_version} not available" >&2
            fi
        done
        echo "" >&2
    fi

    # Update other dependencies, but only to versions compatible with our Go version
    echo "Checking for compatible dependency updates..." >&2
    local other_deps
    other_deps=$(grep -E '^\s+[a-zA-Z0-9]' go.mod | \
                 grep -v '^\s*//' | \
                 awk '{print $1}' | \
                 grep -v '^k8s\.io/' | \
                 grep -v '^sigs\.k8s\.io/controller-runtime' | \
                 grep -v '^github\.com/openshift/' | \
                 sort -u)

    local total=0
    for dep in $other_deps; do
        total=$((total + 1))
    done

    echo "  Found $total other dependencies to check for updates..." >&2
    local counter=0
    local updated=0
    local skipped=0

    for dep in $other_deps; do
        counter=$((counter + 1))

        # Save current go.mod and go.sum
        cp go.mod go.mod.backup
        cp go.sum go.sum.backup

        # Try to update with -u flag (latest version)
        if go get -u "$dep" >/dev/null 2>&1; then
            # Run go mod tidy to see if dependencies are compatible
            if go mod tidy >/dev/null 2>&1; then
                # Check if the go directive changed (means deps require newer Go)
                local new_go_version
                new_go_version=$(grep '^go ' go.mod | awk '{print $2}')

                if [[ "$new_go_version" != "$go_version" ]]; then
                    # Go version changed, this dependency requires newer Go - revert
                    mv go.mod.backup go.mod
                    mv go.sum.backup go.sum
                    skipped=$((skipped + 1))
                else
                    # Go version unchanged, update is compatible
                    rm go.mod.backup go.sum.backup
                    updated=$((updated + 1))
                fi
            else
                # go mod tidy failed, dependency update broke something - revert
                mv go.mod.backup go.mod
                mv go.sum.backup go.sum
                skipped=$((skipped + 1))
            fi
        else
            # go get -u failed (latest version incompatible)
            # Restore from backup and try to find latest compatible patch version
            mv go.mod.backup go.mod
            mv go.sum.backup go.sum

            # Try to find and update to latest compatible patch version
            if try_update_to_latest_compatible "$dep" "$go_version"; then
                updated=$((updated + 1))
            else
                # No compatible update found, keep current version
                skipped=$((skipped + 1))
            fi
        fi

        # Show progress every 10 deps
        if (( counter % 10 == 0 )); then
            echo "  Progress: $counter/$total dependencies checked..." >&2
        fi
    done

    # Clean up any remaining backup files
    rm -f go.mod.backup go.sum.backup

    echo "  ✅ Updated $updated/$total dependencies (skipped $skipped incompatible)" >&2
    echo "" >&2

    # Run go mod tidy and verify
    echo "Running final go mod tidy..." >&2
    go mod tidy

    # Verify go directive wasn't changed by go mod tidy
    local actual_go_version
    actual_go_version=$(grep '^go ' go.mod | awk '{print $2}')

    if [[ "$actual_go_version" != "$go_version" ]]; then
        echo "❌ ERROR: go mod tidy changed go directive from $go_version to $actual_go_version" >&2
        echo "   This means some dependencies require Go >= $actual_go_version" >&2
        echo "   The K8s/OCP versions you specified may not be compatible with Go $go_version" >&2
        echo "" >&2
        echo "Options:" >&2
        echo "  1. Use a BUILD_IMAGE with Go $actual_go_version or newer" >&2
        echo "  2. Use older K8s/OCP versions compatible with Go $go_version" >&2
        return 1
    fi

    echo "✅ go directive verified: $go_version (all dependencies compatible)" >&2
    echo "" >&2

    # Format go.mod to ensure clean structure (2 blocks: direct + indirect)
    echo "Formatting go.mod structure..." >&2
    go mod edit -fmt

    # Verify we have exactly 2 require blocks
    local require_count
    require_count=$(grep -c "^require (" go.mod || true)
    if [[ "$require_count" -ne 2 ]]; then
        echo "⚠️  Found $require_count require blocks, consolidating to 2..." >&2

        # Create temporary file to rebuild go.mod
        local temp_gomod=$(mktemp)

        # Copy everything before first require block
        awk '/^require \(/{exit} {print}' go.mod > "$temp_gomod"

        # Extract direct dependencies (no // indirect)
        echo "require (" >> "$temp_gomod"
        grep -A 10000 "^require (" go.mod | \
            grep -v "^require (" | \
            grep -v "^)" | \
            grep -v "// indirect" | \
            sort -u >> "$temp_gomod" || true
        echo ")" >> "$temp_gomod"
        echo "" >> "$temp_gomod"

        # Extract indirect dependencies (with // indirect)
        echo "require (" >> "$temp_gomod"
        grep -A 10000 "^require (" go.mod | \
            grep "// indirect" | \
            sort -u >> "$temp_gomod" || true
        echo ")" >> "$temp_gomod"

        # Replace go.mod
        mv "$temp_gomod" go.mod

        # Format again
        go mod edit -fmt

        echo "   ✅ Consolidated to 2 require blocks" >&2
    else
        echo "✅ go.mod structure verified: 2 require blocks (direct + indirect)" >&2
    fi
    echo "" >&2

    echo "Running go mod verify..." >&2
    if ! go mod verify; then
        echo "ERROR: go mod verify failed" >&2
        echo "" >&2
        echo "This usually means dependencies require a newer Go version than $go_version" >&2
        echo "Checking for incompatible dependencies..." >&2

        # Try to identify problematic dependencies
        go list -m all 2>&1 | grep -i "requires go" || true

        return 1
    fi

    echo "✅ go.mod verification passed" >&2
    echo "" >&2

    # Final check: ensure go directive matches container version exactly
    actual_go_version=$(grep '^go ' go.mod | awk '{print $2}')
    if [[ "$actual_go_version" != "$go_version" ]]; then
        echo "⚠️  Final fix: Restoring go directive from $actual_go_version to $go_version (container version)..." >&2
        update_go_mod_directive "$go_version"
        echo "✅ go directive finalized: $go_version" >&2
    fi
    echo "" >&2

    # Commit changes
    echo "Committing changes..." >&2
    git add go.mod go.sum
    git commit -m "$(cat <<EOF
pin K8S API to v${k8s_version} and set go minimum version to ${go_version}

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"

    echo "✅ Step 3 complete" >&2
}

# ==============================================================================
# Step 4: Update vendor folder
# ==============================================================================
step_update_vendor() {
    local go_version="$1"

    echo "======================================" >&2
    echo "Step 4: Update vendor folder" >&2
    echo "======================================" >&2
    echo "" >&2

    echo "Using Go version: $go_version" >&2
    echo "" >&2

    # Set GOTOOLCHAIN to use the specified version
    export GOTOOLCHAIN="go${go_version}"
    echo "Setting GOTOOLCHAIN=go${go_version} to use correct Go version" >&2
    echo "" >&2

    # Run go mod tidy to ensure go.mod is in sync
    # Note: go mod tidy may upgrade the go directive if dependencies require it
    echo "Running go mod tidy to ensure go.mod is in sync..." >&2
    go mod tidy

    echo "" >&2

    echo "Removing existing vendor directory..." >&2
    rm -rf vendor/

    echo "Running go mod vendor..." >&2
    go mod vendor

    # After vendoring is complete, restore the go directive to match the container version
    # The vendor directory doesn't depend on the go directive - only builds do
    local actual_go_version
    actual_go_version=$(grep '^go ' go.mod | awk '{print $2}')

    if [[ "$actual_go_version" != "$go_version" ]]; then
        echo "" >&2
        echo "⚠️  Go directive was upgraded to $actual_go_version (required by dependencies)" >&2
        echo "   Restoring to container version $go_version for build compatibility..." >&2
        update_go_mod_directive "$go_version"
        echo "✅ Restored go directive to $go_version" >&2
    else
        echo "✅ go directive is already at container version: $go_version" >&2
    fi
    echo "" >&2

    echo "Committing changes..." >&2
    git add vendor/ go.mod go.sum
    git commit -m "$(cat <<'EOF'
go mod vendor

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"

    echo "✅ Step 4 complete" >&2
}

# ==============================================================================
# Step 5: Run code generation
# ==============================================================================
step_run_code_generation() {
    echo "======================================" >&2
    echo "Step 5: Run code generation" >&2
    echo "======================================" >&2
    echo "" >&2

    echo "Running make generate..." >&2
    make generate

    echo "" >&2
    echo "Running make manifests..." >&2
    make manifests

    echo "" >&2
    echo "Running make bundle..." >&2
    make bundle

    echo "" >&2

    # Only commit if there are changes
    if [[ -n $(git status --porcelain) ]]; then
        echo "Committing generated code..." >&2
        git add .
        git commit -m "$(cat <<'EOF'
Code generation after upgrade

Co-Authored-By: Claude <noreply@anthropic.com>
EOF
)"
        echo "✅ Step 5 complete (changes committed)" >&2
    else
        echo "✅ Step 5 complete (no changes)" >&2
    fi
}

# ==============================================================================
# Step 6: Run tests and build
# ==============================================================================
step_run_tests() {
    local k8s_version="$1"

    echo "======================================" >&2
    echo "Step 6: Run tests and build" >&2
    echo "======================================" >&2
    echo "" >&2

    echo "Running make docker-build..." >&2
    if ! make docker-build; then
        echo "❌ ERROR: make docker-build failed" >&2
        return 1
    fi
    echo "✅ docker-build passed" >&2

    echo "" >&2
    echo "Running make build..." >&2
    if ! make build; then
        echo "❌ ERROR: make build failed" >&2
        return 1
    fi
    echo "✅ build passed" >&2

    echo "" >&2
    echo "Running make test..." >&2
    if ! make test; then
        echo "❌ ERROR: make test failed" >&2
        return 1
    fi
    echo "✅ tests passed" >&2

    echo "" >&2

    # Only commit if there are code changes
    if [[ -n $(git status --porcelain) ]]; then
        echo "Committing code changes..." >&2
        git add .
        git commit -m "Update code after Golang, pivot to k8s ${k8s_version} and dependencies upgrade"
        echo "✅ Step 6 complete (changes committed)" >&2
    else
        echo "✅ Step 6 complete (no code changes needed)" >&2
    fi
}

# ==============================================================================
# Main orchestration
# ==============================================================================
main() {
    if [[ $# -lt 1 ]]; then
        usage
        exit 1
    fi

    local ocp_version="$1"
    local go_version_arg="${2:-}"
    local k8s_version_arg="${3:-}"

    # Validate we're in the correct repository
    if ! validate_in_operator_repo; then
        exit 1
    fi

    echo "=========================================="
    echo "Multiarch Tuning Operator Upgrade"
    echo "=========================================="
    echo ""
    echo "Target OCP version: $ocp_version"
    echo ""

    # Show current versions for reference
    local current_ocp_version current_go_version current_k8s_version
    current_ocp_version=$(discover_current_ocp_version 2>/dev/null || echo "unknown")
    current_go_version=$(grep '^go ' go.mod 2>/dev/null | awk '{print $2}' || echo "unknown")
    current_k8s_version=$(grep 'k8s.io/apimachinery' go.mod 2>/dev/null | grep -v '//' | awk '{print $2}' | head -1 || echo "unknown")

    echo "Current versions:"
    echo "  Go: $current_go_version"
    echo "  Kubernetes: $current_k8s_version"
    echo "  OpenShift: $current_ocp_version"
    echo ""

    # Discover K8s version from OCP if not specified
    local k8s_version
    if [[ -n "$k8s_version_arg" ]]; then
        echo "Using user-specified K8s version: $k8s_version_arg" >&2
        k8s_version="$k8s_version_arg"
    else
        k8s_version=$(discover_k8s_from_ocp_release "$ocp_version")

        if [[ -z "$k8s_version" ]]; then
            echo "" >&2
            echo "⚠️  Could not discover K8s version for OCP $ocp_version" >&2
            echo "   Please specify K8s version manually:" >&2
            echo "   $0 $ocp_version [go-version] <k8s-version>" >&2
            exit 1
        fi

        echo "✅ Discovered K8s version: $k8s_version (from OCP $ocp_version release)" >&2
    fi
    echo "" >&2

    # Discover or use Go version
    local go_version
    if [[ -n "$go_version_arg" ]]; then
        echo "Using user-specified Go version: $go_version_arg" >&2
        go_version="$go_version_arg"
    else
        echo "Discovering Go version for OCP $ocp_version..." >&2
        # Extract Go minor version from OCP openshift/api go.mod
        local go_minor
        go_minor=$(curl -sf "https://raw.githubusercontent.com/openshift/api/release-$ocp_version/go.mod" | grep '^go ' | awk '{print $2}' | cut -d. -f2)

        if [[ -z "$go_minor" ]]; then
            echo "⚠️  Could not discover Go version, using current: $current_go_version" >&2
            go_version="$current_go_version"
        else
            # Get the latest stable patch version for this Go minor version
            go_version=$(discover_latest_go_patch "$go_minor")
            echo "✅ Discovered Go version: $go_version (latest stable for 1.$go_minor)" >&2
        fi
    fi
    echo "" >&2

    echo "Target versions:"
    echo "  Go: $go_version"
    echo "  Kubernetes: $k8s_version"
    echo "  OpenShift: $ocp_version"
    echo ""

    # Prerequisites
    validate_prerequisites "$ocp_version" "$go_version" "$k8s_version"

    # Discover controller-runtime version
    local controller_runtime_version
    controller_runtime_version=$(discover_controller_runtime_version "$k8s_version" "$go_version")
    echo "" >&2

    # Discover tool versions
    echo "Discovering tool versions..." >&2
    local kustomize_version controller_tools_version setup_envtest_version envtest_k8s_version golint_version

    # Discovery functions return versions without 'v' prefix (except envtest_k8s which returns with 'v')
    # We need to add/remove 'v' to match Makefile format:
    # - KUSTOMIZE_VERSION needs 'v' prefix
    # - CONTROLLER_TOOLS_VERSION needs 'v' prefix
    # - SETUP_ENVTEST_VERSION already has 'release-' prefix
    # - ENVTEST_K8S_VERSION should NOT have 'v' prefix
    # - GOLINT_VERSION needs 'v' prefix
    kustomize_version="v$(discover_kustomize_version "$go_version")"
    controller_tools_version="v$(discover_controller_tools_version "$k8s_version" "$go_version")"
    setup_envtest_version=$(derive_setup_envtest_version "$controller_runtime_version")
    envtest_k8s_version=$(extract_envtest_k8s_version "$controller_runtime_version" | sed 's/^v//')
    golint_version="v$(discover_golangci_lint_version "$go_version")"

    echo "" >&2
    echo "=========================================="
    echo "Version Discovery Complete"
    echo "=========================================="
    echo ""
    echo "Versions to be used:"
    echo "  Go: $go_version"
    echo "  Kubernetes: $k8s_version"
    echo "  OpenShift: $ocp_version"
    echo "  controller-runtime: $controller_runtime_version"
    echo "  kustomize: $kustomize_version"
    echo "  controller-tools: $controller_tools_version"
    echo "  setup-envtest: $setup_envtest_version"
    echo "  envtest-k8s: $envtest_k8s_version"
    echo "  golangci-lint: $golint_version"
    echo ""
    echo "Press Enter to continue or Ctrl+C to abort..."
    read -r

    # Run upgrade steps
    echo ""
    step_update_base_images "$go_version" "$k8s_version" "$ocp_version"
    echo ""
    step_update_tools "$kustomize_version" "$controller_tools_version" "$setup_envtest_version" "$envtest_k8s_version" "$golint_version"
    echo ""
    step_update_go_mod "$k8s_version" "$ocp_version" "$controller_runtime_version" "$go_version"
    echo ""
    step_update_vendor "$go_version"
    echo ""
    step_run_code_generation
    echo ""
    step_run_tests "$k8s_version"

    # Success summary
    echo ""
    echo "=========================================="
    echo "Upgrade Complete!"
    echo "=========================================="
    echo ""
    echo "Commits created:"
    git log --oneline HEAD~7..HEAD 2>/dev/null || git log --oneline -n 7
    echo ""
    echo "⚠️  IMPORTANT: Post-upgrade actions required"
    echo ""
    echo "1. Prow config update (may be needed):"
    echo "   The PR in this repo may need to be paired with one in the Prow config."
    echo "   See example: https://github.com/openshift/release/pull/55728/commits/707fa080a66d8006c4a69e452a4621ed54f67cf6"
    echo "   Check if openshift/release needs golang version updates for CI jobs."
    echo ""
    echo "2. Documentation update:"
    echo "   Update docs/ocp-release.md if you discovered any process changes."
    echo ""
    echo "3. Create PR:"
    echo "   Review the changes and create a pull request when ready."
    echo ""
}

# Run main
main "$@"