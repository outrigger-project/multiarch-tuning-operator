# Upgrade Automation Configuration

This directory contains machine-readable configuration files that define **how to upgrade** Go and Kubernetes versions for the multiarch-tuning-operator. These configs are consumed by automation tools to perform version upgrades consistently and correctly.

## Philosophy

**Dynamic Discovery over Hardcoded Mappings**: This directory provides automated upgrade scripts that:

1. **Discover versions dynamically** - Find compatible versions from authoritative sources (no hardcoded version tables)
2. **Update files consistently** - Apply changes to all required files following established patterns
3. **Execute complete workflow** - Run all upgrade steps in the correct order with validation

This approach ensures:
- ✅ **Single source of truth** - The operator team owns and maintains the upgrade process
- ✅ **No hardcoded versions** - All versions discovered from GitHub releases, go.mod files, OCP mirrors
- ✅ **Reproducible** - Same inputs always produce the same results
- ✅ **Version-controlled** - Changes to the process are tracked with the code

## Files

### [`version-matrix.yaml`](version-matrix.yaml)

**Reference documentation** that describes HOW version compatibility relationships are discovered.

**Key sections:**
- `kubernetes_to_openshift` - How to map K8s versions to OCP versions
- `controller_runtime` - How to find compatible controller-runtime versions
- `go_version_requirements` - How to determine minimum Go version
- `base_images` - How to validate base image availability
- `tool_versions` - How to discover compatible tool versions (kustomize, controller-tools, etc.)
- `dependencies` - How to determine go.mod dependency versions

**Purpose:** Documents the discovery algorithms that are implemented in `scripts/lib/version-discovery.sh`. This YAML is **reference documentation**, not directly consumed by the bash scripts. It serves as:
- Specification for what the bash functions should do
- Documentation for understanding the upgrade logic
- Blueprint for future YAML-driven implementations or other automation tools

### [`file-update-patterns.yaml`](file-update-patterns.yaml)

**Reference documentation** that defines WHAT files to update and HOW to update them.

**Key sections:**
- `base_image_updates` - Regex patterns for updating Dockerfiles, Makefiles, .tekton files
- `go_mod_updates` - How to update go.mod directives
- `makefile_tool_updates` - Which Makefile variables to update
- `commit_structure` - Expected commit messages and file groupings
- `validations` - Pre/post-update validation rules
- `post_upgrade` - Actions required after upgrade (Prow config, docs)

**Purpose:** Documents the file update patterns that are implemented in `scripts/lib/file-updates.sh`. This YAML is **reference documentation**, not directly consumed by the bash scripts. The regex patterns and file lists serve as the specification for what the bash functions implement.

### [`upgrade-workflow.yaml`](upgrade-workflow.yaml)

**Reference documentation** that defines the complete step-by-step upgrade procedure.

**Structure:**
- `inputs` - Required and optional parameters
- `prerequisites` - Validation checks before starting
- `steps` - Ordered list of upgrade steps (1-6)
- `post_workflow` - Actions after main workflow
- `expected_commit_log` - Example of correct commit history

**Purpose:** Documents the workflow that is implemented in `scripts/upgrade.sh`. This YAML is **reference documentation** that describes what each step does and in what order. The actual implementation is in the bash script's `step_*` functions.

### [`scripts/`](scripts/)

**Bash implementation** of the upgrade automation that implements the logic defined in the YAML configs.

**Structure:**
- `upgrade.sh` - Main orchestrator script that runs all upgrade steps in sequence
- `lib/version-discovery.sh` - Version discovery functions (~313 lines)
  - `discover_k8s_from_ocp_release()` - Query OCP mirrors for K8s version
  - `discover_controller_runtime_version()` - Find compatible controller-runtime from GitHub releases
  - `discover_kustomize_version()` - Find compatible kustomize version
  - `discover_controller_tools_version()` - Find compatible controller-tools version
  - `discover_golangci_lint_version()` - Find compatible golangci-lint version
  - And more... (see file for complete list)
- `lib/file-updates.sh` - File update utility functions (~130 lines)
  - `update_ci_operator_yaml()` - Update .ci-operator.yaml Go/OCP versions
  - `update_tekton_files()` - Update .tekton/*.yaml files
  - `update_dockerfile()` - Update Dockerfile base image
  - `update_makefile_build_image()` - Update Makefile BUILD_IMAGE
  - `update_makefile_tool_versions()` - Update tool versions in Makefile
  - And more...
- `lib/validations.sh` - Validation and prerequisite checking (~130 lines)
 
**Usage:**
```bash
cd /path/to/multiarch-tuning-operator
hack/upgrade-automation/scripts/upgrade.sh 4.20                    # Discover Go and K8s from OCP 4.20
hack/upgrade-automation/scripts/upgrade.sh 4.21 1.24               # Specify Go, discover K8s
hack/upgrade-automation/scripts/upgrade.sh 4.21 1.24 1.34.1        # Specify all versions
```

The script will:
1. Validate prerequisites and discover compatible versions
2. Update base images (Dockerfiles, Makefile, .tekton files)
3. Update tools in Makefile
4. Update go.mod and dependencies
5. Run go mod vendor
6. Run code generation (make generate, make manifests, make bundle)
7. Run tests and build
8. Create structured commits for each step

**Important:** The bash scripts implement the logic specified in the YAML reference documentation files, but they do not parse or consume those YAML files directly.

### For the AI Helpers Plugin

The `operator-upgrade:multiarch-tuning-operator-upgrade-versions` command in the [ai-helpers](https://github.com/openshift-eng/ai-helpers) repository reads these configs:

```bash
# The plugin is now a thin orchestrator
/operator-upgrade:multiarch-tuning-operator-upgrade-versions 1.23 1.32.3
```

The plugin:
1. Fetches these YAML files from the operator repo
2. Executes the discovery methods to find compatible versions
3. Applies the file update patterns
4. Follows the workflow steps
5. Creates commits matching the expected structure

### Manual Upgrades

Even for manual upgrades, these configs serve as:
- **Checklist** - Ensure you don't miss any files
- **Reference** - Exact regex patterns and validation rules
- **Documentation** - Why each step is necessary

## Design Decisions

### Why YAML instead of code?

**Pros:**
- ✅ Language-agnostic - Any tool can parse YAML
- ✅ Version-controlled - Changes tracked with operator code
- ✅ Human-readable - Easy to review and understand
- ✅ Declarative - Describes "what" not "how"

**Cons:**
- ⚠️ Requires a parser/interpreter in the automation tool
- ⚠️ Limited expressiveness compared to code

We chose YAML because the benefits outweigh the costs for this use case.

### Why dynamic version discovery?

**Problem:** Hardcoded version mappings become stale.

Example of what we **avoid**:
```yaml
# ❌ BAD - Will be outdated soon
kubernetes_to_openshift:
  "1.32": "4.19"
  "1.33": "4.20"
```

**Solution:** Describe how to discover the mapping:
```yaml
# ✅ GOOD - Always up-to-date
kubernetes_to_openshift:
  method: "check_openshift_api_release_branch"
  repo: "https://github.com/openshift/api"
  branch_pattern: "release-{ocp_version}"
  validate: "k8s.io/api version matches target"
```

This ensures automation tools always get current information from authoritative sources.

### Why separate files?

- `version-matrix.yaml` - **Discovery logic** (how to find versions)
- `file-update-patterns.yaml` - **Transformation logic** (how to update files)
- `upgrade-workflow.yaml` - **Orchestration logic** (what order to do things)

Each file has a single responsibility, making them easier to maintain and update independently.

## Updating These Configs

### When the upgrade process changes

If the upgrade procedure changes (new files to update, new steps, etc.):

1. Update the relevant YAML file(s)
2. Test with the automation tool
3. Commit changes to the operator repo
4. Update `docs/ocp-release.md` to match

### When new OpenShift versions are released

**No changes needed!** The dynamic discovery methods automatically handle new versions.

### When new tools are added

Add the tool to `version-matrix.yaml`:

```yaml
tool_versions:
  new_tool:
    method: "find_latest_compatible"
    api_url: "https://api.github.com/repos/org/tool/releases"
    for_each_release:
      - fetch_file: "https://raw.githubusercontent.com/org/tool/v{version}/go.mod"
      - extract_go_req: "grep '^go ' | awk '{print $2}'"
      - match_criteria: "release_go_minor <= target_go_minor"
```

Add the Makefile variable to `file-update-patterns.yaml`:

```yaml
makefile_tool_updates:
  - variable: "NEW_TOOL_VERSION"
    description: "Description of the tool"
    dynamic: true
    discovery: "Latest version compatible with Go version"
    reference: "https://github.com/org/tool/releases"
```

### When file paths change

Update the `file` entries in `file-update-patterns.yaml`:

```yaml
base_image_updates:
  - file: "new/path/to/Dockerfile"  # Updated path
    # ... rest of config
```

## Validation

To validate the bash scripts and YAML documentation are correct:

1. **Run the bash scripts** - The best validation is successful execution
   ```bash
   hack/upgrade-automation/scripts/upgrade.sh <ocp-version>
   ```
2. **Compare with manual process** - Results should match `../../docs/ocp-release.md`
3. **Check commit structure** - Should match `expected_commit_log` in `upgrade-workflow.yaml`
4. **Verify YAML docs match bash** - Review both when making changes

## Examples

### Example: Find compatible controller-runtime version

Using `version-matrix.yaml`:

```bash
# Target: K8s 1.32, Go 1.23
# Query GitHub API
curl -s https://api.github.com/repos/kubernetes-sigs/controller-runtime/releases

# For each release (e.g., v0.20.0):
curl -s https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.20.0/go.mod

# Check if it uses k8s.io/apimachinery v0.32.x and go 1.23
# If match, use controller-runtime v0.20.0
```

### Example: Update Makefile BUILD_IMAGE

Using `file-update-patterns.yaml`:

```bash
# Pattern: (BUILD_IMAGE\s*\?=\s*registry\.ci\.openshift\.org/ocp/builder:rhel-9-golang-)(\d+\.\d+)((?:-builder-multi)?-openshift-)(\d+\.\d+)
# Replacement: $1{go_version}$3{ocp_version}

# Before:
BUILD_IMAGE ?= registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.22-openshift-4.17

# After:
BUILD_IMAGE ?= registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.23-openshift-4.19
```

## Contributing

To improve these configs:

1. Test changes with the automation tool first
2. Ensure manual process still works
3. Update all three files if the change affects multiple aspects
4. Keep the configs in sync with `docs/ocp-release.md`

## See Also

- [../ocp-release.md](../ocp-release.md) - Human-readable upgrade documentation
- [ai-helpers operator-upgrade plugin](https://github.com/openshift-eng/ai-helpers/tree/main/plugins/operator-upgrade) - Automation tool that uses these configs