#!/bin/bash
# Version discovery functions for multiarch-tuning-operator upgrades
#
# This library provides functions to dynamically discover compatible versions
# for Go, Kubernetes, controller-runtime, and all required tools.
#
# NO HARDCODED VERSIONS - all discovered from authoritative sources.

set -euo pipefail

# Discover K8s version used by OCP from release.txt
discover_k8s_from_ocp_release() {
    local ocp_version="$1"

    echo "Finding K8s version from OCP $ocp_version release..." >&2

    # Try to fetch from OCP release mirror (candidate or stable)
    local k8s_version
    k8s_version=$(curl -sf "https://mirror.openshift.com/pub/openshift-v4/amd64/clients/ocp-dev-preview/candidate-$ocp_version/release.txt" 2>/dev/null | \
        grep -E 'kubernetes ' | head -1 | awk '{print $2}')

    if [[ -n "$k8s_version" ]]; then
        echo "$k8s_version"
    else
        # Fallback: discover from openshift/api go.mod
        local k8s_minor
        k8s_minor=$(curl -sf "https://raw.githubusercontent.com/openshift/api/release-$ocp_version/go.mod" | \
            grep 'k8s.io/api ' | awk '{print $2}' | grep -oP 'v0\.\K[0-9]+')

        if [[ -n "$k8s_minor" ]]; then
            echo "1.$k8s_minor.0"
        else
            echo ""
        fi
    fi
}

# Discover latest stable Go patch version for a given minor version
discover_latest_go_patch() {
    local go_minor="$1"

    echo "Finding latest stable Go 1.$go_minor patch version..." >&2

    # Query go.dev for the latest stable patch release
    local latest_patch
    latest_patch=$(curl -s "https://go.dev/dl/?mode=json&include=all" | \
        grep '"version"' | \
        grep -v 'rc\|beta' | \
        sed -E 's/.*"version":\s*"go([0-9.]+)".*/\1/' | \
        grep "^1\\.${go_minor}\\." | \
        sort -uV | \
        tail -1)

    if [[ -n "$latest_patch" ]]; then
        echo "$latest_patch"
    else
        echo "1.${go_minor}.0"
    fi
}

# Discover required OCP version from target K8s version
# This queries openshift/api branches to find which OCP version uses the target K8s version
discover_required_ocp_version() {
    local k8s_version="$1"
    local k8s_minor
    k8s_minor=$(echo "$k8s_version" | cut -d. -f2)

    echo "Discovering required OCP version for K8s 1.$k8s_minor..." >&2

    # Get all openshift/api release branches
    local branches
    branches=$(curl -s "https://api.github.com/repos/openshift/api/branches" | grep '"name"' | grep 'release-4' | sed -E 's/.*"release-([^"]+)".*/\1/')

    # Check each branch to find which uses our target K8s version
    for ocp_version in $branches; do
        local openshift_api_k8s
        openshift_api_k8s=$(curl -sf "https://raw.githubusercontent.com/openshift/api/release-$ocp_version/go.mod" | grep 'k8s.io/api ' | awk '{print $2}' | grep -oP 'v0\.\K[0-9]+')

        if [[ "$openshift_api_k8s" == "$k8s_minor" ]]; then
            echo "✅ Found OCP $ocp_version uses K8s 1.$k8s_minor" >&2
            echo "$ocp_version"
            return 0
        fi
    done

    echo "ERROR: Could not find OCP version for K8s 1.$k8s_minor" >&2
    echo "  Checked openshift/api release branches" >&2
    return 1
}

# Discover target OCP version from current BUILD_IMAGE
discover_current_ocp_version() {
    local current_ocp_version
    current_ocp_version=$(grep 'BUILD_IMAGE' Makefile | grep -oP 'openshift-\K[0-9]+\.[0-9]+')

    if [[ -z "$current_ocp_version" ]]; then
        echo "ERROR: Could not extract OCP version from BUILD_IMAGE in Makefile" >&2
        return 1
    fi

    echo "$current_ocp_version"
}

# Validate K8s version is compatible with target OCP version
validate_k8s_ocp_compatibility() {
    local k8s_version="$1"
    local ocp_version="$2"

    local k8s_minor
    k8s_minor=$(echo "$k8s_version" | cut -d. -f2)

    echo "Validating K8s $k8s_version is compatible with OCP $ocp_version..." >&2

    # Check openshift/api release branch for K8s version
    local openshift_api_k8s
    openshift_api_k8s=$(curl -sf "https://raw.githubusercontent.com/openshift/api/release-$ocp_version/go.mod" | grep 'k8s.io/api ' | awk '{print $2}' | grep -oP 'v0\.\K[0-9]+')

    if [[ "$openshift_api_k8s" != "$k8s_minor" ]]; then
        echo "ERROR: K8s version mismatch" >&2
        echo "  Target: K8s 1.$k8s_minor.x" >&2
        echo "  OCP $ocp_version uses: K8s 1.$openshift_api_k8s.x" >&2
        echo "  Update target K8s version to match OCP $ocp_version" >&2
        return 1
    fi

    echo "✅ K8s $k8s_version is compatible with OCP $ocp_version" >&2
    return 0
}

# Discover compatible controller-runtime version
discover_controller_runtime_version() {
    local k8s_version="$1"
    local go_version="$2"

    local k8s_minor go_minor
    k8s_minor=$(echo "$k8s_version" | cut -d. -f2)
    go_minor=$(echo "$go_version" | cut -d. -f2)

    echo "Discovering compatible controller-runtime version..." >&2

    local releases
    releases=$(curl -s https://api.github.com/repos/kubernetes-sigs/controller-runtime/releases | grep '"tag_name"' | grep -E '"v0\.' | sed -E 's/.*"v([^"]+)".*/\1/')

    for version in $releases; do
        local gomod
        gomod=$(curl -sf "https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v${version}/go.mod")

        if [[ -z "$gomod" ]]; then
            continue
        fi

        local cr_k8s_minor cr_go_minor
        cr_k8s_minor=$(echo "$gomod" | grep 'k8s.io/apimachinery' | awk '{print $2}' | grep -oP 'v0\.\K[0-9]+' | head -1)
        cr_go_minor=$(echo "$gomod" | grep '^go ' | awk '{print $2}' | cut -d. -f2)

        if [[ "$cr_k8s_minor" == "$k8s_minor" ]] && [[ "$cr_go_minor" -le "$go_minor" ]]; then
            echo "✅ Found controller-runtime v$version (k8s.io v0.$cr_k8s_minor, Go 1.$cr_go_minor)" >&2
            echo "$version"
            return 0
        fi
    done

    echo "ERROR: Could not find compatible controller-runtime version" >&2
    return 1
}

# Discover compatible kustomize version
discover_kustomize_version() {
    local go_version="$1"
    local go_minor
    go_minor=$(echo "$go_version" | cut -d. -f2)

    echo "Discovering compatible kustomize version..." >&2

    local releases
    releases=$(curl -s https://api.github.com/repos/kubernetes-sigs/kustomize/releases | grep '"tag_name"' | grep 'kustomize/v5' | sed -E 's/.*"kustomize\/v([^"]+)".*/\1/')

    for version in $releases; do
        local go_req
        go_req=$(curl -sf "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/kustomize/v${version}/kustomize/go.mod" | grep '^go ' | awk '{print $2}' | cut -d. -f2)

        if [[ -n "$go_req" ]] && [[ "$go_req" -le "$go_minor" ]]; then
            echo "✅ Found kustomize v$version (requires Go 1.$go_req)" >&2
            echo "$version"
            return 0
        fi
    done

    # Fallback to current version
    local current_version
    current_version=$(grep 'KUSTOMIZE_VERSION' Makefile | grep -oP 'v\K[0-9]+\.[0-9]+\.[0-9]+')
    echo "⚠️  Using current version from Makefile: v$current_version" >&2
    echo "$current_version"
}

# Discover compatible controller-tools version
discover_controller_tools_version() {
    local k8s_version="$1"
    local go_version="$2"

    local k8s_minor go_minor
    # Extract minor version from k8s_version parameter (e.g., "1.34.1" -> "34")
    k8s_minor=$(echo "$k8s_version" | grep -oP '1\.\K[0-9]+')
    go_minor=$(echo "$go_version" | cut -d. -f2)

    echo "Discovering compatible controller-tools version..." >&2

    local releases
    releases=$(curl -s https://api.github.com/repos/kubernetes-sigs/controller-tools/releases | grep '"tag_name"' | grep -E '"v0\.' | sed -E 's/.*"v([^"]+)".*/\1/')

    for version in $releases; do
        local gomod
        gomod=$(curl -sf "https://raw.githubusercontent.com/kubernetes-sigs/controller-tools/v${version}/go.mod")

        if [[ -z "$gomod" ]]; then
            continue
        fi

        local ct_k8s_minor ct_go_minor
        ct_k8s_minor=$(echo "$gomod" | grep 'k8s.io/apimachinery' | awk '{print $2}' | grep -oP 'v0\.\K[0-9]+' | head -1)
        ct_go_minor=$(echo "$gomod" | grep '^go ' | awk '{print $2}' | cut -d. -f2)

        if [[ "$ct_k8s_minor" == "$k8s_minor" ]] && [[ "$ct_go_minor" -le "$go_minor" ]]; then
            echo "✅ Found controller-tools v$version (k8s.io v0.$ct_k8s_minor, Go 1.$ct_go_minor)" >&2
            echo "$version"
            return 0
        fi
    done

    # Fallback to current version
    local current_version
    current_version=$(grep 'CONTROLLER_TOOLS_VERSION' Makefile | grep -oP 'v\K[0-9]+\.[0-9]+\.[0-9]+')
    echo "⚠️  Using current version from Makefile: v$current_version" >&2
    echo "$current_version"
}

# Derive setup-envtest version from controller-runtime version
derive_setup_envtest_version() {
    local controller_runtime_version="$1"

    local minor
    minor=$(echo "$controller_runtime_version" | cut -d. -f2)

    local version="release-0.${minor}"
    echo "Setup-envtest version: $version (from controller-runtime v0.${minor}.x)" >&2

    # Verify tag exists
    local exists
    exists=$(curl -sf "https://api.github.com/repos/kubernetes-sigs/controller-runtime/git/refs/tags/${version}" | grep '"ref"')

    if [[ -z "$exists" ]]; then
        echo "⚠️  WARNING: $version tag not found in controller-runtime" >&2
        echo "   This may cause setup-envtest installation to fail" >&2
    fi

    echo "$version"
}

# Extract envtest K8s version from controller-runtime's dependencies
extract_envtest_k8s_version() {
    local controller_runtime_version="$1"

    echo "Extracting envtest K8s version from controller-runtime v$controller_runtime_version..." >&2

    local gomod
    gomod=$(curl -sf "https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v${controller_runtime_version}/go.mod")

    if [[ -z "$gomod" ]]; then
        echo "ERROR: Could not fetch controller-runtime go.mod" >&2
        return 1
    fi

    local version
    version=$(echo "$gomod" | grep 'k8s.io/api ' | awk '{print $2}')

    echo "✅ ENVTEST_K8S_VERSION=$version (from controller-runtime's k8s.io/api)" >&2
    echo "$version"
}

# Discover compatible golangci-lint version
discover_golangci_lint_version() {
    local go_version="$1"
    local go_minor
    go_minor=$(echo "$go_version" | cut -d. -f2)

    echo "Discovering compatible golangci-lint version..." >&2

    local releases
    releases=$(curl -s https://api.github.com/repos/golangci/golangci-lint/releases | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')

    for version in $releases; do
        local go_req
        go_req=$(curl -sf "https://raw.githubusercontent.com/golangci/golangci-lint/v${version}/go.mod" | grep '^go ' | awk '{print $2}' | cut -d. -f2)

        if [[ -n "$go_req" ]] && [[ "$go_req" -le "$go_minor" ]]; then
            echo "✅ Found golangci-lint v$version (requires Go 1.$go_req)" >&2
            echo "$version"
            return 0
        fi
    done

    # Fallback to current version
    local current_version
    current_version=$(grep 'GOLINT_VERSION' Makefile | grep -oP 'v\K[0-9]+\.[0-9]+\.[0-9]+')
    echo "⚠️  Using current version from Makefile: v$current_version" >&2
    echo "$current_version"
}
