package base_plugin_test

import (
	operatorplugins "github.com/openshift/multiarch-tuning-operator/apis/multiarch/common/plugins"
	baseplugins2 "github.com/openshift/multiarch-tuning-operator/apis/multiarch/common/plugins/base_plugin"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/common/plugins/nodeaffinityscoring"
	"testing"

	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1alpha1"
	"github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestV1Alpha1ToV1Beta1Conversion(t *testing.T) {
	// Create a v1alpha1 object
	v1alpha1Obj := &v1alpha1.ClusterPodPlacementConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cppc",
			Namespace: "default",
		},
		Spec: v1alpha1.ClusterPodPlacementConfigSpec{
			LogVerbosity:      "Normal",
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "test"}},
			Plugins: operatorplugins.OperatorPlugins{
				NodeAffinityScoring: &nodeaffinityscoring.NodeAffinityScoring{
					BasePlugin: baseplugins2.BasePlugin{
						Enabled: true,
					},
					Platforms: []nodeaffinityscoring.NodeAffinityScoringPlatformTerm{
						{Architecture: "ppc64le", Weight: 50},
					},
				},
			},
		},
	}

	// Convert to v1beta1
	v1beta1Obj := &v1beta1.ClusterPodPlacementConfig{}
	err := v1alpha1Obj.ConvertTo(v1beta1Obj)
	if err != nil {
		t.Fatalf("Failed to convert v1alpha1 to v1beta1: %v", err)
	}

	// Validate the conversion
	if v1beta1Obj.Name != v1alpha1Obj.Name {
		t.Errorf("Name mismatch: expected %s, got %s", v1alpha1Obj.Name, v1beta1Obj.Name)
	}

	if v1beta1Obj.Spec.Plugins.NodeAffinityScoring == nil {
		t.Fatalf("NodeAffinityScoring plugin is nil in v1beta1 object")
	}

	if v1beta1Obj.Spec.Plugins.NodeAffinityScoring.Platforms[0].Architecture != "ppc64le" {
		t.Errorf("Architecture mismatch: expected %s, got %s",
			"ppc64le", v1beta1Obj.Spec.Plugins.NodeAffinityScoring.Platforms[0].Architecture)
	}

}

func TestV1Alpha1WithNoPluginsField(t *testing.T) {
	// Create a v1alpha1 object with no base_plugins field
	v1alpha1Obj := &v1alpha1.ClusterPodPlacementConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cppc-no-base_plugins",
			Namespace: "default",
		},
		Spec: v1alpha1.ClusterPodPlacementConfigSpec{
			LogVerbosity:      "Normal",
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"env": "test"}},
		},
	}

	// Convert to v1beta1
	v1beta1Obj := &v1beta1.ClusterPodPlacementConfig{}
	err := v1alpha1Obj.ConvertTo(v1beta1Obj)
	if err != nil {
		t.Fatalf("Failed to convert v1beta1 to v1alpha1: %v", err)
	}

	// Convert v1beta1 to itself and ensure it works without modification
	v1beta1ObjClone := &v1beta1.ClusterPodPlacementConfig{}
	err = v1alpha1Obj.ConvertTo(v1beta1ObjClone)
	if err != nil {
		t.Fatalf("Failed to convert v1beta1 to v1beta1: %v", err)
	}

	// Validate the conversion back to v1beta1
	if v1beta1ObjClone.Name != v1beta1Obj.Name {
		t.Errorf("Name mismatch in v1beta1 conversion: expected %s, got %s", v1beta1Obj.Name, v1beta1ObjClone.Name)
	}

	if v1beta1ObjClone.Spec.Plugins != v1beta1Obj.Spec.Plugins {
		t.Errorf("Expected nil base_plugins in v1beta1, got %v", v1beta1ObjClone.Spec.Plugins)
	}
}

func TestV1Alpha1WithNodeAffinityScoringPluginDisabled(t *testing.T) {
	// Create a v1alpha1 object with NodeAffinityScoring plugin set, enabled = false, no other keys
	v1alpha1Obj := &v1alpha1.ClusterPodPlacementConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cppc-na-disabled",
			Namespace: "default",
		},
		Spec: v1alpha1.ClusterPodPlacementConfigSpec{
			LogVerbosity: "Normal",
			Plugins: operatorplugins.OperatorPlugins{
				NodeAffinityScoring: &nodeaffinityscoring.NodeAffinityScoring{
					BasePlugin: baseplugins2.BasePlugin{
						Enabled: false,
					},
					Platforms: nil, // No additional configuration
				},
			},
		},
	}

	// Convert to v1beta1
	v1beta1Obj := &v1beta1.ClusterPodPlacementConfig{}
	err := v1alpha1Obj.ConvertTo(v1beta1Obj)
	if err != nil {
		t.Fatalf("Failed to convert v1alpha1 to v1beta1: %v", err)
	}

	// Validate the conversion for v1beta1
	if v1beta1Obj.Spec.Plugins.NodeAffinityScoring == nil {
		t.Fatalf("NodeAffinityScoring plugin should not be nil in v1alpha1")
	}
	if v1beta1Obj.Spec.Plugins.NodeAffinityScoring.Enabled {
		t.Errorf("Expected NodeAffinityScoring plugin to be disabled in v1alpha1, but got enabled")
	}
	if v1beta1Obj.Spec.Plugins.NodeAffinityScoring.Platforms != nil {
		t.Errorf("Expected nil Platforms in v1alpha1, but got %v", v1alpha1Obj.Spec.Plugins.NodeAffinityScoring.Platforms)
	}

	// Convert back to v1beta1
	v1beta1ObjClone := &v1beta1.ClusterPodPlacementConfig{}
	err = v1alpha1Obj.ConvertTo(v1beta1ObjClone)
	if err != nil {
		t.Fatalf("Failed to convert v1alpha1 to v1beta1: %v", err)
	}

	// Validate the conversion back to v1beta1
	if v1beta1ObjClone.Spec.Plugins.NodeAffinityScoring == nil {
		t.Fatalf("NodeAffinityScoring plugin should not be nil in v1beta1")
	}
	if v1beta1ObjClone.Spec.Plugins.NodeAffinityScoring.Enabled {
		t.Errorf("Expected NodeAffinityScoring plugin to be disabled in v1beta1, but got enabled")
	}
	if v1beta1ObjClone.Spec.Plugins.NodeAffinityScoring.Platforms != nil {
		t.Errorf("Expected nil Platforms in v1beta1, but got %v", v1beta1ObjClone.Spec.Plugins.NodeAffinityScoring.Platforms)
	}
}

func TestV1Alpha1WithEmptyNodeAffinityScoringPlatforms(t *testing.T) {
	// Create a v1alpha1 object with empty Platforms in NodeAffinityScoring plugin
	v1alpha1Obj := &v1alpha1.ClusterPodPlacementConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cppc-empty-platforms",
			Namespace: "default",
		},
		Spec: v1alpha1.ClusterPodPlacementConfigSpec{
			LogVerbosity: "Normal",
			Plugins: operatorplugins.OperatorPlugins{
				NodeAffinityScoring: &nodeaffinityscoring.NodeAffinityScoring{
					BasePlugin: baseplugins2.BasePlugin{
						Enabled: true,
					},
					Platforms: []nodeaffinityscoring.NodeAffinityScoringPlatformTerm{},
				},
			},
		},
	}

	// Convert to v1beta1
	v1beta1Obj := &v1beta1.ClusterPodPlacementConfig{}
	err := v1alpha1Obj.ConvertTo(v1beta1Obj)
	if err != nil {
		t.Fatalf("Failed to convert v1alpha1 to v1beta1: %v", err)
	}

	// Validate the conversion for v1beta1
	if v1beta1Obj.Spec.Plugins.NodeAffinityScoring == nil {
		t.Fatalf("NodeAffinityScoring plugin should not be nil in v1beta1")
	}
	if len(v1beta1Obj.Spec.Plugins.NodeAffinityScoring.Platforms) != 0 {
		t.Errorf("Expected empty Platforms in v1beta1, but got %v", v1beta1Obj.Spec.Plugins.NodeAffinityScoring.Platforms)
	}

	// Convert back to v1beta1cone
	v1alpha1Clone := &v1alpha1.ClusterPodPlacementConfig{}
	err = v1alpha1Clone.ConvertFrom(v1beta1Obj)
	if err != nil {
		t.Fatalf("Failed to convert v1alpha1 to v1beta1: %v", err)
	}

	// Validate the conversion back to v1beta1
	if v1alpha1Clone.Spec.Plugins.NodeAffinityScoring == nil {
		t.Fatalf("NodeAffinityScoring plugin should not be nil in v1beta1")
	}
	if len(v1alpha1Clone.Spec.Plugins.NodeAffinityScoring.Platforms) != 0 {
		t.Errorf("Expected empty Platforms in v1beta1, but got %v", v1alpha1Clone.Spec.Plugins.NodeAffinityScoring.Platforms)
	}
}

func TestV1Alpha1WithNonEmptyNodeAffinityScoringPlatforms(t *testing.T) {
	// Create a v1alpha1 object with non-empty Platforms in NodeAffinityScoring plugin
	v1alpha1Obj := &v1alpha1.ClusterPodPlacementConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cppc-nonempty-platforms",
			Namespace: "default",
		},
		Spec: v1alpha1.ClusterPodPlacementConfigSpec{
			LogVerbosity: "Normal",
			Plugins: operatorplugins.OperatorPlugins{
				NodeAffinityScoring: &nodeaffinityscoring.NodeAffinityScoring{
					BasePlugin: baseplugins2.BasePlugin{
						Enabled: true,
					},
					Platforms: []nodeaffinityscoring.NodeAffinityScoringPlatformTerm{
						{
							Architecture: "ppc64le",
							Weight:       10,
						},
						{
							Architecture: "amd64",
							Weight:       20,
						},
					},
				},
			},
		},
	}

	// Convert to v1beta1
	v1beta1Obj := &v1beta1.ClusterPodPlacementConfig{}
	err := v1alpha1Obj.ConvertTo(v1beta1Obj)
	if err != nil {
		t.Fatalf("Failed to convert v1alpha1 to v1beta1: %v", err)
	}

	// Validate the conversion for v1beta1
	if v1beta1Obj.Spec.Plugins.NodeAffinityScoring == nil {
		t.Fatalf("NodeAffinityScoring plugin should not be nil in v1beta1")
	}
	if len(v1beta1Obj.Spec.Plugins.NodeAffinityScoring.Platforms) != 2 {
		t.Errorf("Expected 2 Platforms in v1beta1, but got %v", len(v1beta1Obj.Spec.Plugins.NodeAffinityScoring.Platforms))
	}
	if v1beta1Obj.Spec.Plugins.NodeAffinityScoring.Platforms[0].Architecture != "ppc64le" ||
		v1beta1Obj.Spec.Plugins.NodeAffinityScoring.Platforms[0].Weight != 10 {
		t.Errorf("First platform in v1beta1 does not match expected values")
	}
	if v1beta1Obj.Spec.Plugins.NodeAffinityScoring.Platforms[1].Architecture != "amd64" ||
		v1beta1Obj.Spec.Plugins.NodeAffinityScoring.Platforms[1].Weight != 20 {
		t.Errorf("Second platform in v1beta1 does not match expected values")
	}
}
