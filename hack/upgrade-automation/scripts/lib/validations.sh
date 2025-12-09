#!/bin/bash
# Validation functions for multiarch-tuning-operator upgrades
#
# This library provides validation functions to ensure upgrade safety
# and operator-specific requirements.

set -euo pipefail

# Validate we're on the right branch and create upgrade branch
validate_and_create_branch() {
    local ocp_version="$1"
    local go_version="$2"
    local k8s_version="$3"

    # First, validate working directory is clean
    if ! validate_clean_working_directory; then
        return 1
    fi

    echo "" >&2
    echo "Validating git branch..." >&2

    # Get current branch
    local current_branch
    current_branch=$(git branch --show-current)

    # Determine main branch (usually 'main' or 'master')
    local main_branch
    if git show-ref --verify --quiet refs/heads/main; then
        main_branch="main"
    elif git show-ref --verify --quiet refs/heads/master; then
        main_branch="master"
    else
        echo "ERROR: Could not find main or master branch" >&2
        return 1
    fi

    # If not on main branch, confirm with user
    if [[ "$current_branch" != "$main_branch" ]]; then
        echo "⚠️  WARNING: You are on branch '$current_branch', not '$main_branch'" >&2
        echo "" >&2
        read -p "Do you want to switch to '$main_branch' and pull latest changes? (y/N) " -n 1 -r >&2
        echo "" >&2
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            echo "Switching to $main_branch..." >&2
            git checkout "$main_branch"
            echo "Pulling latest changes..." >&2
            git pull
            current_branch="$main_branch"
        else
            echo "Continuing on branch '$current_branch'" >&2
        fi
    fi

    # Create upgrade branch
    local go_minor k8s_minor
    go_minor=$(echo "$go_version" | cut -d. -f1-2)
    k8s_minor=$(echo "$k8s_version" | cut -d. -f1-2)

    local branch_name="upgrade-ocp-${ocp_version}-go-${go_minor}-k8s-${k8s_minor}"

    echo "Creating upgrade branch: $branch_name" >&2

    # Check if branch already exists
    if git show-ref --verify --quiet "refs/heads/$branch_name"; then
        echo "" >&2
        echo "⚠️  WARNING: Branch '$branch_name' already exists" >&2
        echo "" >&2
        read -p "Do you want to delete and recreate it? (y/N) " -n 1 -r >&2
        echo "" >&2
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            git branch -D "$branch_name"
            git checkout -b "$branch_name"
        else
            echo "ERROR: Branch '$branch_name' already exists. Please delete it or choose a different version." >&2
            return 1
        fi
    else
        git checkout -b "$branch_name"
    fi

    echo "✅ Created and checked out branch: $branch_name" >&2
    return 0
}

# Validate working directory is clean (deprecated - use validate_and_create_branch instead)
validate_clean_working_directory() {
    echo "Validating clean working directory..." >&2

    if [[ -n $(git status --porcelain) ]]; then
        echo "ERROR: Working directory is not clean" >&2
        echo "Commit or stash changes before running upgrade" >&2
        git status --short >&2
        return 1
    fi

    echo "✅ Working directory is clean" >&2
    return 0
}

# Validate base image exists (checks minor version only)
validate_golang_image_exists() {
    local go_version="$1"
    local go_minor
    go_minor=$(echo "$go_version" | cut -d. -f1-2)

    echo "Validating golang:$go_minor image exists..." >&2

    # Check if the minor version tag exists (e.g., golang:1.24)
    if docker manifest inspect "golang:$go_minor" >/dev/null 2>&1; then
        echo "✅ golang:$go_minor image exists" >&2
        echo "$go_version"
        return 0
    fi

    echo "ERROR: golang:$go_minor image not found" >&2
    echo "  The exact patch version ($go_version) will be set in go.mod" >&2
    echo "  But the Dockerfile will use golang:$go_minor" >&2
    return 1
}

# Validate go.mod k8s.io version consistency
validate_k8s_version_consistency() {
    echo "Validating k8s.io version consistency in go.mod..." >&2

    local versions_count
    versions_count=$(grep '^\s*k8s\.io/' go.mod | grep -v '//' | awk '{print $2}' | grep -oP 'v\d+\.\d+' | sort -u | wc -l)

    if [[ "$versions_count" -gt 1 ]]; then
        echo "⚠️  WARNING: Multiple k8s.io minor versions detected in go.mod" >&2
        echo "   Found versions:" >&2
        grep '^\s*k8s\.io/' go.mod | grep -v '//' | awk '{print $1, $2}' | grep -oP 'v\d+\.\d+' | sort -u >&2
        echo "   This is expected for packages like k8s.io/klog and k8s.io/utils" >&2
    else
        echo "✅ All k8s.io dependencies at consistent minor version" >&2
    fi

    return 0
}

# Validate all step prerequisites
validate_prerequisites() {
    local ocp_version="$1"
    local go_version="$2"
    local k8s_version="$3"

    echo "==================================" >&2
    echo "Running prerequisite validations" >&2
    echo "==================================" >&2
    echo "" >&2

    # Validate branch and create upgrade branch
    if ! validate_and_create_branch "$ocp_version" "$go_version" "$k8s_version"; then
        return 1
    fi

    echo "" >&2

    # Extract current versions for comparison
    local current_go current_k8s current_ocp
    current_go=$(grep '^go ' go.mod | awk '{print $2}')
    current_k8s=$(grep 'k8s.io/api' go.mod | head -1 | awk '{print $2}')
    current_ocp=$(grep 'BUILD_IMAGE' Makefile | grep -oP 'openshift-\K[0-9]+\.[0-9]+')

    echo "Current versions:" >&2
    echo "  Go: $current_go" >&2
    echo "  Kubernetes: $current_k8s" >&2
    echo "  OpenShift: $current_ocp" >&2
    echo "" >&2
    echo "Target versions:" >&2
    echo "  Go: $go_version" >&2
    echo "  Kubernetes: $k8s_version" >&2
    echo "" >&2

    return 0
}
