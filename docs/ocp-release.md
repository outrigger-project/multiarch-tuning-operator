# Release process for OCP
* [ProdSec considerations](#prodsec-considerations)
* [Pin to a new Golang and K8S API version](#pin-to-a-new-golang-and-k8s-api-version)
* [Branching and creation of a new development streams](#branching-and-creation-of-a-new-development-stream)
  * [Development stream for the Operator](#development-stream-for-the-operator)
  * [Development stream for the FBC](#development-stream-for-the-fbc)
    * [Create a new FBC fragment for a new OCP release](#create-a-new-fbc-fragment-for-a-new-ocp-release)
* [Release the operator (for OCP)](#release-process-for-ocp)
* [Publish a bundle in the OCP's OperatorHub's FBCs](#publish-a-bundle-in-the-ocps-operatorhubs-fbcs)\
* [Example Release](#example-release)

## ProdSec considerations

If the release is a major version or introduced significant
[architectural changes](https://docs.engineering.redhat.com/pages/viewpage.action?pageId=402429315),
ensure that the threat model is reviewed and updated accordingly.

SAST findings will be processed according to:

- https://spaces.redhat.com/display/PRODSEC/SAST+Workflow
- https://spaces.redhat.com/display/PRODSEC/Weakness+Management+Standard
- https://spaces.redhat.com/display/PRODSEC/Security+Errata+Standard

In general, SAST findings cause a PR to fail to merge.
It's the resposibility of the PR author to review the findings.

In case a finding is not a false positive, it needs to be classified as weakness or vulnerability
(if in doubt, contact the Security Architect for help).
Once classified, an appropriate Jira issue needs to be created (ask Security Architect for guidance).

The issues will have to be remediated according to the timelines set in the standard.

If malware is detected, contact prodsec@redhat.com immediately.

## Pin to a new Golang and K8S API version

1. Update go version in base images 
   - Update the Dockerfiles to use a base image with the desired Golang version (it should be the one used by k8s.io/api
   or openshift/api)
   - The function `getCorrectHostmountAnyUIDSCC` relies on the Kubernetes version to determine which SCC is used for the `podplacementconfig` pod's hostPath mounts. Please double-check the code to ensure that the Kubernetes version aligns with the corresponding OpenShift version and has not been inadvertently upgraded or downgraded.
   - Update the Makefile to use the new Golang version base image (BUILD_IMAGE variable)
   - Check if updated references are needed in .tekton for konflux 
   - Commit the changes to the Golang version
4. Update go.mod
   - Update the k8s libraries in the go.mod file to the desired version 
   - Update the dependencies with `go get -u`, and ensure no new version of the k8s API is used
    ```shell
    go mod download
    go mod tidy
    go mod verify
    ```
   - Commit the changes to go.mod and go.sum
7. Update the vendor/ folder and commit changes 

```shell
rm -rf vendor/
go mod vendor
```
4. Update the tools in the Makefile to the desired version and commit changes:

```makefile
# https://github.com/kubernetes-sigs/kustomize/releases
KUSTOMIZE_VERSION ?= v5.4.3
# https://github.com/kubernetes-sigs/controller-tools/releases
CONTROLLER_TOOLS_VERSION ?= v0.16.1
# https://github.com/kubernetes-sigs/controller-runtime/branches
SETUP_ENVTEST_VERSION ?= release-0.18
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.29.3
# https://github.com/golangci/golangci-lint/releases
GOLINT_VERSION = v1.60.1
```

5. Run and commit the following. Address any warnings or errors that may occur
```shell
make generate
make manifests
make bundle
```
6. Run the tests and ensure everything is building and working as expected. Look for deprecation warnings PRs in the
    controller-runtime repository.
```shell
make docker-build
make build
make test
```

7. Commit any other changes to the code, if any
8. Create a PR with the changes.
9. Update this document with any changes 

Example log:

```
02eb25dd (HEAD -> update-go) Add info in the ocp-release.md doc about k8s and golang upgrade
8e2d3389 Add info in the ocp-release.md doc about k8s and golang upgrade
46e7b338 Update code after Golang, pivot to k8s 1.30.4 and dependencies upgrade
787dfb2a Update tools in Makefile
289ebaa2 go mod vendor
cda73fe1 pin K8S API to v0.30.4 and set go minimum version to 1.22.5
e511fdce Update go version in base images to 1.22
```

Example PRs: https://github.com/openshift/multiarch-tuning-operator/pull/225, https://github.com/openshift/multiarch-tuning-operator/pull/542

The PR in the repo may need to be paired with one in the Prow config:
see https://github.com/openshift/release/pull/55728/commits/707fa080a66d8006c4a69e452a4621ed54f67cf6 as an example

## Branching and creation of a new development stream
### Development stream for the operator
This is done at the beginning of a release cycle.

1. Create new branch off of main. For example v1.x, v1.0, v0.9. 
2. Define a new Development Stream for the operator in the [Konflux
   cluster](https://gitlab.cee.redhat.com/releng/konflux-release-data). <br><br>
   Create a new directory
   `tenants-config/cluster/stone-prd-rh01/tenants/multiarch-tuning-ope-tenant/_base/projectl.konflux.dev/projectdevelopmentstreams/multiarch-tuning-operator-<konfluxVersion>`.
   For instance, when the branch version is `v1.0`, replace the period (`.`) with a dash (`-`). Thus, `<konfluxVersion>` would
   be formatted as `v1-0`.
   Add two files to the directory `projectdevelopmentstream.yaml` and `kustomization.yaml`.

   ```yaml
   # multiarch-tuning-operator-<konfluxVersion>/projectdevelopmentstream.yaml
   ---
   apiVersion: projctl.konflux.dev/v1beta1
   kind: ProjectDevelopmentStream 
   metadata:
     name: multiarch-tuning-operator-<konfluxVersion>
   spec:

     project: multiarch-tuning-operator
     template:
       name: multiarch-tuning-operator
       values:
         - name: version
           # The branch name, formatted without the 'v' prefix (e.g., '1.0' for branch 'v1.0')
           value: <versionName>
   ```

   ```yaml
   # multiarch-tuning-operator-<konfluxVersion>/kustomization.yaml
   ---
   kind: Kustomization
   apiVersion: kustomize.config.k8s.io/v1beta1
   resources:
     - ./projectdevelopmentstream.yaml
   ```

   Update `tenants-config/cluster/stone-prd-rh01/tenants/multiarch-tuning-ope-tenant/_base/kustomization.yaml` file to
   include the newly added `projectdevelopmentstreams`.<br><br>
   Run `build-manifests.sh` to generate auxiliary files. After making the changes, create a new pull request.

3. Update the tekton files in the newly created branch. An example can be found [here](https://github.com/openshift/multiarch-tuning-operator/commit/b31311f3e642b2ba4ac71b32a675395962d5dd38).

4. Duplicate the prow config to target the new branch. Add
   `ci-operator/config/openshift/multiarch-tuning-operator/openshift-multiarch-tuning-operator-<branchVersion>.yaml` by
   copping `openshift-multiarch-tuning-operator-main.yaml` and updating references from `main` to the new branch name.
   (ex. https://github.com/openshift/release/pull/56835)
5. Create a new `ReleasePlanAdmission` file in the directory
      `konflux-release-data/config/stone-prd-rh01.pg1f.p1/product/ReleasePlanAdmission/multiarch-tuning-ope`,
      naming it `multiarch-tuning-operator-<version>.yaml` where `<version>` is the `konfluxVersion` without the "
      v" (e.g., for `v1-0`, name it `multiarch-tuning-operator-1-0.yaml`).
      Duplicate the contents of the existing ReleasePlanAdmission from the `multiarch-tuning-operator.yaml` file, update
      the
    - tags
    - set the appropriate release notes version
    - change metadata name from `multiarch-tuning-operator` to `multiarch-tuning-operator-<version>`
    - change the spec application name to `multiarch-tuning-operator-<version>`
    - change the components to include `<version>`<br><br>
      Run `build-manifests.sh` to generate auxiliary files. After making the changes, create a new pull request.

### Development stream for the FBC
This is done when a new OCP version is added.

1. Define a new Development Stream for the operator in the [Konflux
   cluster](https://gitlab.cee.redhat.com/releng/konflux-release-data). <br><br>
   Create a new directory
   `tenants-config/cluster/stone-prd-rh01/tenants/multiarch-tuning-ope-tenant/_base/projectl.konflux.dev/projectdevelopmentstreams/fbc-<ocp-version> `.
   For instance, when the branch version is v4.18, replace the period (.) with a dash (-). Thus, <ocp-version> would be formatted as v4-18.
   Add two files to the directory `projectdevelopmentstream.yaml` and `kustomization.yaml`.

   ```yaml
   # multiarch-tuning-operator-<konfluxVersion>/projectdevelopmentstream.yaml
   ---
   apiVersion: projctl.konflux.dev/v1beta1
   kind: ProjectDevelopmentStream 
   metadata:
     name: file-based-catalog-<ocp-version>
   spec:

     project: multiarch-tuning-operator
     template:
       name: file-based-catalog
       values:
         - name: version
           # The version name, formatted without the 'v' prefix (e.g., '4.18' for version `v4.18')
           value: <version>
   ```

   ```yaml
   # multiarch-tuning-operator-<konfluxVersion>/kustomization.yaml
   ---
   kind: Kustomization
   apiVersion: kustomize.config.k8s.io/v1beta1
   resources:
     - ./projectdevelopmentstream.yaml
   ```

   Update `tenants-config/cluster/stone-prd-rh01/tenants/multiarch-tuning-ope-tenant/_base/kustomization.yaml` file to
   include the newly added `projectdevelopmentstreams`.<br><br>
   Run `build-manifests.sh` to generate auxiliary files.
2. Update the `ReleasePlanAdmission` for both [multiarch-tuning-operator-fbc-staging-index.yaml](https://gitlab.cee.redhat.com/releng/konflux-release-data/-/blob/main/config/stone-prd-rh01.pg1f.p1/product/ReleasePlanAdmission/multiarch-tuning-ope/multiarch-tuning-operator-fbc-staging-index.yaml) and [multiarch-tuning-operator-fbc-prod-index.yaml](https://gitlab.cee.redhat.com/releng/konflux-release-data/-/blob/main/config/stone-prd-rh01.pg1f.p1/product/ReleasePlanAdmission/multiarch-tuning-ope/multiarch-tuning-operator-fbc-prod-index.yaml) by adding the new `fbc-<ocp-version>` to `spec.applications`. 
#### Create a new FBC fragment for a new OCP release
1. Create a new directory in [fbc branch](https://github.com/outrigger-project/multiarch-tuning-operator/tree/fbc) called `fbc-v4-x/catalog/multiarch-tuning-operator`
   Copy contents from the most recent ocp versions `index.yaml` into the newly created directory's `index.yaml`. <br>
   Do not copy from fbc-v4-16 and do not change the contents of the yaml file.
3. Update the tekton files by adding a new `fbc-v4-x-pull-request.yaml` and `fbc-v4-x-push.yaml` <br><br>
4. Adjust the base-image argument for the `build-args` parameter in the Konflux PipelineRun.
5. Adjust the graphs as needed in the branches.
   An example pr can be found [here](https://github.com/outrigger-project/multiarch-tuning-operator/pull/642)

## Release the operator (for OCP)

1. Bump the operator version

To bump the operator version, run the following commands:

```shell
make version VERSION=1.0.1
./hack/bump-version.sh
```

 Verify that no other references to the previous versions are present in the codebase.
If so, update `hack/bump-version.sh` to include any further patches required to update the version.

1. When planning to release a new version (e.g. v1.0) or a patch version (e.g., v1.0.z), select a snapshot from the
corresponding Konflux application. This snapshot should be on-push that has the latest commit we want to release.
Ensure all post-submit checks, pipeline tasks, and enterprise contract tests have successfully passed.
Log any warnings if they have occurred ([example](https://redhat-internal.slack.com/archives/C06ECDGH4LE/p1761098980898099?thread_ts=1760127394.479789&cid=C06ECDGH4LE)).<br><br>
Check if the snapshot contains the right images. There are two ways to do this.
   - Run `./hack/check-snapshots.sh` and skip to step 4
   - Manually check with the following command and steps  2 and 3
 ```shell
 oc get snapshots --sort-by .metadata.creationTimestamp -l pac.test.appstudio.openshift.io/event-type=push,appstudio.openshift.io/application=multiarch-tuning-operator-<version>
 ```
2. Look at the results of the tests for the commit reported in the snapshot:
```yaml
# [ ... ]
spec:
 application: multiarch-tuning-operator
 artifacts: { }
 components:
   - containerImage: quay.io/redhat-user-workloads/multiarch-tuning-ope-tenant/multiarch-tuning-operator-<version>/multiarch-tuning-operator-<version>@sha256:250498c137c91f8f932317d48ecacb0d336e705828d3a4163e684933b610547f
     name: multiarch-tuning-operator-<version>
     source:
       git:
         revision: d73959c925629f29f9071fd6e7d58a0f58a54399
         url: https://github.com/openshift/multiarch-tuning-operator
   - containerImage: quay.io/redhat-user-workloads/multiarch-tuning-ope-tenant/multiarch-tuning-operator-<version>/multiarch-tuning-operator-bundle-<version>@sha256:be0945723a0a5ad881d135b3f83a65b0e8fc69c0da337339587aebed4bee89a1
     name: multiarch-tuning-operator-bundle-<version>
     source:
       git:
         context: ./
         dockerfileUrl: bundle.Dockerfile
         revision: d73959c925629f29f9071fd6e7d58a0f58a54399
         url: https://github.com/openshift/multiarch-tuning-operator
# [ ... ]
```
3. Ensure that the `containerImage` of the operator matches the one referenced by the bundle in the selected snapshot.
   This value should not be updated manually—Konflux will automatically file the PR. If it doesn’t, there may be an
   issue with the nudging process, and you should review the `Renovate` PipelineRun logs. Once resolved, wait for the new snapshot to be made
   and restart at step 2. <br><br>
   Pull and save the pulled image to a tar file. Extract the tar file into the newly created directory and remember to extract the nested tar files.
```shell
find /tmp/operator_bundle -name "*.clusterserviceversion.yaml" -print
  yq '.spec.install.spec.deployments[0].spec.template.metadata.annotations."multiarch.openshift.io/image"' <file-from-above-command>
```

4. Update the [allowed tags](https://gitlab.cee.redhat.com/releng/konflux-release-data/-/blob/main/config/stone-prd-rh01.pg1f.p1/product/ReleasePlanAdmission/multiarch-tuning-ope/multiarch-tuning-operator-1-0.yaml#L27-28) for the `ReleasePlanAdmission` 
```yaml
# ...
defaults:
  tags:
    - "1.0.0"
    - "1.0.0-{{ timestamp }}"
    - # append new tags here
# ...
```
6. In [comet](https://comet.engineering.redhat.com/containers/repositories/6616582895a35187a06ba2ce) add the new tag in the content streams field
7. Create a new release for the operator. The description should come from the docs team.
```yaml
apiVersion: appstudio.redhat.com/v1alpha1
kind: Release
metadata:
  generateName: release-1-x-x>-
  namespace: multiarch-tuning-ope-tenant
spec:
  releasePlan: multiarch-tuning-operator-x-release-as-operator
  snapshot: <snapshot>
  data:
    releaseNotes:
      type: RHEA
      synopsis: Red Hat Multiarch Tuning 1.x.x
      issues:
          fixed:
          - id: MULTIARCH-xxxx
            source: issues.redhat.com
      topic: >-
        The 1.x.x release of the Red Hat Multiarch Tuning Operator.
        For more details, see product documentation in the references section.
      description: >-
        Enhancements:
        * List of features
        Bug fixes:
        * List of fixes
      solution: |
        The Multiarch Tuning Operator optimizes workload management within multi-architecture clusters and in single-architecture clusters transitioning to multi-architecture environments.
        This Operator is available in the Red Hat Operators catalog that is included with OpenShift Container Platform.
        For more details, see product documentation in the references section.
      references:
        - https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/postinstallation_configuration/configuring-multi-architecture-compute-machines-on-an-openshift-cluster#multiarch-tuning-operator
        - https://github.com/outrigger-project/multiarch-tuning-operator
        - https://github.com/openshift/openshift-docs/blob/989810607bedb7d3c727fb55907355de2ba80af8/post_installation_configuration/configuring-multi-arch-compute-machines/multi-arch-tuning-operator-release-notes.adoc
        - https://github.com/openshift/openshift-docs/blob/main/post_installation_configuration/configuring-multi-arch-compute-machines/multiarch-tuning-operator.adoc
```
8. Watch the `status` of the `Release` Object or look in the Konflux UI to confirm that images and bundle were published
    as expected. Note that the peipeline is run in a different namespace (rhtap-releng-tenant).

## Publish a bundle in the OCP's OperatorHub's FBCs
Note: these steps need to be applied to each FBC

1. Download opm from https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/latest-4.<latest OCP version>/
1. Update the `fbc-4.x` directories in the [fbc branch](https://github.com/outrigger-project/multiarch-tuning-operator/tree/fbc) to use the selected snapshot.
   Append the rendered output of the snapshot to each `index.yaml`. There are two methods.
   2. Run `/hack/build-indexs.sh <snapshot>` and `./hack/update-graph.sh <operator-version>`
   3. Append the new package generated with the command below and set the nodes and upgrade edges in the related channel graph.
   
```shell
opm render --output=yaml --migrate-level bundle-object-to-csv-metadata <konflux-bundle-image>
# Remove --migrate-level bundle-object-to-csv-metadata for FBCs that target OCP version <=4.16
```

    Make a pull request with the updated `index.yaml`. Follow the template below for you commit message:

```
Add v1.1.0
 
Snapshot: multiarch-tuning-operator-1-0-hw4gh

spec:
  application: multiarch-tuning-operator-1-0
  artifacts: {}
  components:
  - containerImage: quay.io/redhat-user-workloads/multiarch-tuning-ope-tenant/multiarch-tuning-operator-1-0/multiarch-tuning-operator-1-0@sha256:1a1d3f5258d4b91f1a0380b0fe2c809823e323d9ff74ae4171468168d255e7e0
    name: multiarch-tuning-operator-1-0
    source:
      git:
        context: ./
        dockerfileUrl: Dockerfile
        revision: ba0559f8f8721de872c7779684cce4726747e7b7
        url: https://github.com/openshift/multiarch-tuning-operator
  - containerImage: quay.io/redhat-user-workloads/multiarch-tuning-ope-tenant/multiarch-tuning-operator-1-0/multiarch-tuning-operator-bundle-1-0@sha256:3932cb10204a8d87c2b07b16be193d605567a3d0b9dab8cbc021317e83c551f5
    name: multiarch-tuning-operator-bundle-1-0
    source:
      git:
        revision: a1078df7300d74b60ddff2c3b1dd5f890700d89b
        url: https://github.com/openshift/multiarch-tuning-operator

```
   Regardless of the method commit the changes in a [pr](https://github.com/openshift/multiarch-tuning-operator/pull/54). 

9. Get the new snapshots triggered by the build of the merge commit from the pr created in the previous step and check the build pipeline for errors and warnings. Document any warnings. 

    ```shell
    oc get snapshots --sort-by .metadata.creationTimestamp -l pac.test.appstudio.openshift.io/event-type=push,appstudio.openshift.io/application=fbc-v4-x
    ```

10. Create a new Release for the fbc snapshot created after the commit in the previous step. 
    Note that we can run a staging release to test the release pipeline

    ```yaml
    # oc create -f - <<EOF
    apiVersion: appstudio.redhat.com/v1alpha1
    kind: Release
    metadata:
      generateName: manual-release-<operator-version>-
      namespace: multiarch-tuning-ope-tenant
    spec:
      releasePlan: fbc-4-x-release-as-fbc # fbc-v4-16-release-as-staging-fbc is available for a staging release
      snapshot: fbc-<new-fbc-snapshot>
    # EOF
    ```
11. Watch the `status` of the `Release` Object or look in the Konflux UI to confirm that images and bundle were published
as expected. Note that the peipeline is run in a different namespace (rhtap-releng-tenant).

12. Once the release pipeline has succeeded view the index images provided in the task run section.
    ```yaml
    {
        "index_image": {
            "index_image": "registry-proxy.engineering.redhat.com/rh-osbs/iib:835019",
            "index_image_resolved": "registry-proxy.engineering.redhat.com/rh-osbs/iib@sha256:d594746527f8acd02af12c48d15842f02cf61a3091aede7238d222f1d0ec92c5"
        }
    }
    ```
    Take the index image and replace the registry `registry-proxy.engineering.redhat.com/rh-osbs/iib` with  `brew.registry.redhat.io`.
    The image to announce would look like `brew.registry.redhat.io/rh-osbs/iib:835019`


## Example Release
### Pre Development Work
Create the epic and tasks for the upcoming release these will help guide and document the steps to be taken for the release. 
Remember to link the epics/stories related to this release.
The [tasks will differ](https://spaces.redhat.com/spaces/ENTMQMAAS/pages/151038604/Creating+a+new+micro+Z-stream+release) depending on what type of release it is (x/y/z stream).
Epics are [PLMPGM-3819](https://issues.redhat.com/browse/PLMPGM-3819) and
[PLMPGM-3872](https://issues.redhat.com/browse/PLMPGM-3872)

Follow the [branching and creation of a new development streams](#branching-and-creation-of-a-new-development-stream). 
### Post Development Work
When development is nearing its end, set a release date in task 5 and start planing task 1. 
Once the development work is complete follow the epic release steps for the administrative, security, and documentation tasks. 

To release follow: 
* [Release the operator (for OCP)](#release-process-for-ocp)
* [Publish a bundle in the OCP's OperatorHub's FBCs](#publish-a-bundle-in-the-ocps-operatorhubs-fbcs)

Note: These steps are happening simultaneously.

The general steps are:
* Cherry-pick changes for the release onto release branch 
* Get an operator snapshot
  * validate all the pipeline runs and document the digests 
* Run the FBC staging release
* Follow up with docs
* Have QE and ProdSpec validate the fbc release 
* Run the operator release 
* Update the tags in git and internal release frameworks 
  * Check the epic tasks for other steps
* Run all the FBC releases on the release date

 For examples:
 * [v1.1.1](https://redhat-internal.slack.com/archives/C06ECDGH4LE/p1761070678851309)
 * [v1.2.0](https://redhat-internal.slack.com/archives/C06ECDGH4LE/p1760127394479789)