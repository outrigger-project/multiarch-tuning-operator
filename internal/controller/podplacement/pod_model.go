/*
Copyright 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package podplacement

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	"github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/internal/controller/podplacement/metrics"
	"github.com/openshift/multiarch-tuning-operator/pkg/image"
	"github.com/openshift/multiarch-tuning-operator/pkg/models"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var (
	// imageInspectionCache is the facade singleton used to inspect images. It is defined here to facilitate testing.
	imageInspectionCache image.ICache = image.FacadeSingleton()
)

const MaxRetryCount = 5

type containerImage struct {
	imageName string
	skipCache bool
}

type Pod struct {
	models.Pod
}

func newPod(pod *corev1.Pod, ctx context.Context, recorder record.EventRecorder) *Pod {
	return &Pod{
		Pod: *models.NewPod(pod, ctx, recorder),
	}
}

// HasSchedulingGate checks if the pod has the scheduling gate utils.SchedulingGateName.
func (pod *Pod) HasSchedulingGate() bool {
	return pod.HasGate(utils.SchedulingGateName)
}

// RemoveSchedulingGate removes the scheduling gate utils.SchedulingGateName from the pod.
func (pod *Pod) RemoveSchedulingGate() {
	pod.RemoveGate(utils.SchedulingGateName)
	// The scheduling gate is removed. We also add a label to the pod to indicate that the scheduling gate was removed
	// and this pod was processed by the operator. That's useful for testing and debugging, but also gives the user
	// an indication that the pod was processed by the operator.
	pod.EnsureLabel(utils.SchedulingGateLabel, utils.SchedulingGateLabelValueRemoved)
}

// ensureSchedulingGate ensures that the pod has the scheduling gate utils.SchedulingGateName.
func (pod *Pod) ensureSchedulingGate() {
	pod.AddGate(utils.SchedulingGateName)
}

// getPodImagePullSecrets returns the names of the image pull secrets used by the pod.
func (pod *Pod) getPodImagePullSecrets() []string {
	if pod.Spec.ImagePullSecrets == nil {
		// If the imagePullSecrets array is nil, return emptylist
		return []string{}
	}
	var secretRefs = make([]string, len(pod.Spec.ImagePullSecrets))
	for i, secret := range pod.Spec.ImagePullSecrets {
		secretRefs[i] = secret.Name
	}
	return secretRefs
}

// SetNodeAffinityArchRequirement wraps the logic to set the nodeAffinity for the pod.
// It verifies first that no nodeSelector field is set for the kubernetes.io/arch label.
// Then, it computes the intersection of the architectures supported by the images used by the pod via pod.getArchitecturePredicate.
// Finally, it initializes the nodeAffinity for the pod and set it to the computed requirement via the pod.setRequiredArchNodeAffinity method.
func (pod *Pod) SetNodeAffinityArchRequirement(pullSecretDataList [][]byte) (bool, error) {
	if pod.isNodeSelectorConfiguredForArchitecture() {
		pod.publishIgnorePod()
		return false, nil
	}
	requirement, err := pod.getArchitecturePredicate(pullSecretDataList)
	if err != nil {
		return false, err
	}
	pod.EnsureNoLabel(utils.ImageInspectionErrorLabel)
	if len(requirement.Values) == 0 {
		pod.PublishEvent(corev1.EventTypeNormal, NoSupportedArchitecturesFound, NoSupportedArchitecturesFoundMsg)
	}
	pod.ensureArchitectureLabels(requirement)

	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = &corev1.Affinity{}
	}

	if pod.Spec.Affinity.NodeAffinity == nil {
		pod.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	}

	if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{}
	}

	pod.setRequiredArchNodeAffinity(requirement)
	return true, nil
}

// setRequiredArchNodeAffinity sets the node affinity for the pod to the given requirement based on the rules in
// the sig-scheduling's KEP-3838: https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/3838-pod-mutable-scheduling-directives.
func (pod *Pod) setRequiredArchNodeAffinity(requirement corev1.NodeSelectorRequirement) {
	// the .requiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms are ORed
	if len(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) == 0 {
		// We create a new array of NodeSelectorTerm of length 1 so that we can always iterate it in the next.
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = make([]corev1.NodeSelectorTerm, 1)
	}
	nodeSelectorTerms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms

	// The expressions within the nodeSelectorTerms are ANDed.
	// Therefore, we iterate over the nodeSelectorTerms and add an expression to each of the terms to verify the
	// kubernetes.io/arch label has compatible values.
	// Note that the NodeSelectorTerms will always be long at least 1, because we (re-)created it with size 1 above if it was nil (or having 0 length).
	var skipMatchExpressionPatch bool
	for i := range nodeSelectorTerms {
		skipMatchExpressionPatch = false
		if nodeSelectorTerms[i].MatchExpressions == nil {
			nodeSelectorTerms[i].MatchExpressions = make([]corev1.NodeSelectorRequirement, 0, 1)
		}
		// Check if the nodeSelectorTerm already has a matchExpression for the kubernetes.io/arch label.
		// if yes, we ignore to add it.
		for _, expression := range nodeSelectorTerms[i].MatchExpressions {
			if expression.Key == requirement.Key {
				skipMatchExpressionPatch = true
				break
			}
		}
		// if skipMatchExpressionPatch is true, we skip to add the matchExpression so that conflictual matchExpressions provided by the user are not overwritten.
		if !skipMatchExpressionPatch {
			nodeSelectorTerms[i].MatchExpressions = append(nodeSelectorTerms[i].MatchExpressions, requirement)
		}
	}
	// if the nodeSelectorTerms were patched at least once, we set the nodeAffinity label to the set value, to keep
	// track of the fact that the nodeAffinity was patched by the operator.
	pod.EnsureLabel(utils.NodeAffinityLabel, utils.NodeAffinityLabelValueSet)
	pod.PublishEvent(corev1.EventTypeNormal, ArchitectureAwareNodeAffinitySet,
		ArchitecturePredicateSetupMsg+fmt.Sprintf("{%s}", strings.Join(requirement.Values, ", ")))
}

// SetPreferredArchNodeAffinity sets the node affinity for the pod to the preferences given in the ClusterPodPlacementConfig.
func (pod *Pod) SetPreferredArchNodeAffinity(cppc *v1beta1.ClusterPodPlacementConfig) {
	// Prevent overriding of user-provided kubernetes.io/arch preferred affinities or overwriting previously set preferred affinity
	if pod.isPreferredAffinityConfiguredForArchitecture() {
		return
	}

	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = &corev1.Affinity{}
	}

	if pod.Spec.Affinity.NodeAffinity == nil {
		pod.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	}

	if pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil {
		pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution = []corev1.PreferredSchedulingTerm{}
	}

	for _, nodeAffinityScoringPlatformTerm := range cppc.Spec.Plugins.NodeAffinityScoring.Platforms {
		preferredSchedulingTerm := corev1.PreferredSchedulingTerm{
			Weight: nodeAffinityScoringPlatformTerm.Weight,
			Preference: corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      utils.ArchLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{nodeAffinityScoringPlatformTerm.Architecture},
					},
				},
			},
		}
		pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(
			pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution, preferredSchedulingTerm)
	}

	// if the nodeSelectorTerms were patched at least once, we set the nodeAffinity label to the set value, to keep
	// track of the fact that the nodeAffinity was patched by the operator.
	pod.EnsureLabel(utils.PreferredNodeAffinityLabel, utils.NodeAffinityLabelValueSet)
	pod.PublishEvent(corev1.EventTypeNormal, ArchitectureAwareNodeAffinitySet, ArchitecturePreferredPredicateSetupMsg)
}

func (pod *Pod) getArchitecturePredicate(pullSecretDataList [][]byte) (corev1.NodeSelectorRequirement, error) {
	architectures, err := pod.intersectImagesArchitecture(pullSecretDataList)
	// if an error occurs, we return an empty NodeSelectorRequirement and the error.
	if err != nil {
		return corev1.NodeSelectorRequirement{}, err
	}

	if len(architectures) == 0 {
		return corev1.NodeSelectorRequirement{
			Key:      utils.NoSupportedArchLabel,
			Operator: corev1.NodeSelectorOpExists,
		}, nil
	}
	return corev1.NodeSelectorRequirement{
		Key:      utils.ArchLabel,
		Operator: corev1.NodeSelectorOpIn,
		Values:   architectures,
	}, nil
}

func (pod *Pod) imagesNamesSet() sets.Set[containerImage] {
	imageNamesSet := sets.New[containerImage]()
	for _, container := range append(pod.Spec.Containers, pod.Spec.InitContainers...) {
		imageNamesSet.Insert(containerImage{
			imageName: fmt.Sprintf("//%s", container.Image),
			skipCache: container.ImagePullPolicy == corev1.PullAlways,
		})
	}
	return imageNamesSet
}

// inspect returns the list of supported architectures for the images used by the pod.
// if an error occurs, it returns the error and a nil slice of strings.
func (pod *Pod) intersectImagesArchitecture(pullSecretDataList [][]byte) (supportedArchitectures []string, err error) {
	log := ctrllog.FromContext(pod.Ctx())
	imageNamesSet := pod.imagesNamesSet()
	log.V(1).Info("Images list for pod", "imageNamesSet", fmt.Sprintf("%+v", imageNamesSet))
	// https://github.com/containers/skopeo/blob/v1.11.1/cmd/skopeo/inspect.go#L72
	// Iterate over the images, get their architectures and intersect (as in set intersection) them each other
	var supportedArchitecturesSet sets.Set[string]
	nowExternal := time.Now()
	defer utils.HistogramObserve(nowExternal, metrics.TimeToInspectPodImages)
	for imageContainer := range imageNamesSet {
		log.V(3).Info("Checking image", "imageName", imageContainer.imageName,
			"skipCache (imagePullPolicy==Always)", imageContainer.skipCache)
		// We are collecting the time to inspect the image here to avoid implementing a metric in each of the
		// cache implementations.
		now := time.Now()
		currentImageSupportedArchitectures, err := imageInspectionCache.GetCompatibleArchitecturesSet(pod.Ctx(),
			imageContainer.imageName, imageContainer.skipCache, pullSecretDataList)
		utils.HistogramObserve(now, metrics.TimeToInspectImage)
		if err != nil {
			log.V(1).Error(err, "Error inspecting the image", "imageName", imageContainer.imageName)
			return nil, err
		}
		if supportedArchitecturesSet == nil {
			supportedArchitecturesSet = currentImageSupportedArchitectures
		} else {
			supportedArchitecturesSet = supportedArchitecturesSet.Intersection(currentImageSupportedArchitectures)
		}
	}
	return sets.List(supportedArchitecturesSet), nil
}

func (pod *Pod) maxRetries() bool {
	if pod.Labels == nil {
		return false
	}
	v, err := strconv.ParseInt(pod.Labels[utils.ImageInspectionErrorCountLabel], 10, 32)
	if err != nil {
		return true
	}
	return v >= MaxRetryCount
}

// ensureArchitectureLabels adds labels for the given requirement to the pod. Labels are added to indicate
// the supported architectures and index pods by architecture or by whether they support more than one architecture.
// In this case, single-architecture is meant as a pod that supports only one architecture: all the images in the pod
// may be manifest-list, but the intersection of the architectures is a single value.
func (pod *Pod) ensureArchitectureLabels(requirement corev1.NodeSelectorRequirement) {
	if requirement.Values == nil {
		return
	}
	switch len(requirement.Values) {
	case 0:
		// if the requirement has no values, we set the NoSupportedArchLabel as a label for the node. That's a dummy
		// and non-available-by-default label that we use to prevent the pod from being scheduled when it cannot run all
		// the containers in at least one architecture.
		pod.EnsureLabel(utils.NoSupportedArchLabel, "")
	case 1:
		pod.EnsureLabel(utils.SingleArchLabel, "")
	default:
		pod.EnsureLabel(utils.MultiArchLabel, "")
	}
	for _, value := range requirement.Values {
		pod.EnsureLabel(utils.ArchLabelValue(value), "")
	}
}

// shouldIgnorePod returns true if the pod should be ignored by the operator.
// The operator should ignore the pods in the following cases:
// - the pod is in the same namespace as the operator
// - the pod is in a namespace with prefix kube-
// - the pod has a node name set
// - the pod has a node selector that matches the control plane nodes
// - the pod is owned by a DaemonSet
// - both the nodeSelector/nodeAffinity and the preferredAffinity are set for the kubernetes.io/arch label.
// - only the nodeSelector/nodeAffinity is set for the kubernetes.io/arch label and the NodeAffinityScoring plugin is disabled.
func (pod *Pod) shouldIgnorePod(cppc *v1beta1.ClusterPodPlacementConfig) bool {
	return utils.Namespace() == pod.Namespace || strings.HasPrefix(pod.Namespace, "kube-") ||
		pod.Spec.NodeName != "" || pod.HasControlPlaneNodeSelector() || pod.IsFromDaemonSet() ||
		pod.isNodeSelectorConfiguredForArchitecture() &&
			(!cppc.PluginsEnabled(common.NodeAffinityScoringPluginName) || pod.isPreferredAffinityConfiguredForArchitecture())
}

// isNodeSelectorConfiguredForArchitecture returns true if the pod has already a nodeSelector for the architecture label
// or if all the nodeSelectorTerms in the nodeAffinity field have a matchExpression for the architecture label.
func (pod *Pod) isNodeSelectorConfiguredForArchitecture() bool {
	// if the pod has the nodeSelector field set for the kubernetes.io/arch label, we ignore it.
	// in fact, the nodeSelector field is ANDed with the nodeAffinity field, and we want to give the user the main control, if they
	// manually set a predicate for the kubernetes.io/arch label.
	// The same behavior is implemented below within each
	// nodeSelectorTerm's MatchExpressions field.
	for key := range pod.Spec.NodeSelector {
		if key == utils.ArchLabel {
			pod.publishIgnorePod()
			return true
		}
	}
	// Check if Affinity, NodeAffinity, or RequiredDuringSchedulingIgnoredDuringExecution is nil
	// If any of these are nil, assume there are no specific node selector terms to check, so return true.
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil || pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return false
	}

	// Iterate over NodeSelectorTerms (terms are ORed)
	for _, nodeSelectorTerm := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		// Assume the architecture label is not present
		hasArchLabel := false

		// Check all match expressions within the current NodeSelectorTerm (expressions are ANDed)
		for _, matchExpression := range nodeSelectorTerm.MatchExpressions {
			// If we find the architecture label, mark it as found
			if matchExpression.Key == utils.ArchLabel {
				hasArchLabel = true
				break
			}
		}

		// If one of the NodeSelectorTerms does not have the architecture label, return false
		if !hasArchLabel {
			return false
		}

	}

	// If all NodeSelectorTerms contain the architecture label, return true
	return true
}

func (pod *Pod) publishIgnorePod() {
	log := ctrllog.FromContext(pod.Ctx())
	log.V(1).Info("The pod has the nodeSelector or all the nodeAffinityTerms set for the kubernetes.io/arch label. Ignoring the pod...")
	pod.EnsureLabel(utils.NodeAffinityLabel, utils.LabelValueNotSet)
	pod.PublishEvent(corev1.EventTypeNormal, ArchitecturePredicatesConflict, ArchitecturePredicatesConflictMsg)
}

func (pod *Pod) handleError(err error, s string) {
	if err == nil {
		return
	}
	log := ctrllog.FromContext(pod.Ctx())
	metrics.FailedInspectionCounter.Inc()
	pod.EnsureLabel(utils.ImageInspectionErrorLabel, "")
	pod.EnsureAnnotation(utils.ImageInspectionErrorLabel, err.Error())
	pod.EnsureAndIncrementLabel(utils.ImageInspectionErrorCountLabel)
	pod.PublishEvent(corev1.EventTypeWarning, ImageArchitectureInspectionError, ImageArchitectureInspectionErrorMsg+err.Error())
	log.Error(err, s)
}

// isPreferredAffinityConfiguredForArchitecture returns true if the pod has a MatchExpression in the PreferredDuringSchedulingIgnoredDuringExecution
// that matches kubernetes.io/arch
func (pod *Pod) isPreferredAffinityConfiguredForArchitecture() bool {
	if pod.Spec.Affinity == nil ||
		pod.Spec.Affinity.NodeAffinity == nil ||
		pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil {
		return false
	}

	for _, term := range pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
		for _, expr := range term.Preference.MatchExpressions {
			if expr.Key == utils.ArchLabel {
				return true
			}
		}
	}
	return false
}
