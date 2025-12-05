package v1beta1

import (
	"testing"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_conditionFromBool(t *testing.T) {
	type args struct {
		b bool
	}
	tests := []struct {
		name string
		args args
		want v1.ConditionStatus
	}{
		{
			name: "Test conditionFromBool with true",
			args: args{b: true},
			want: v1.ConditionTrue,
		},
		{
			name: "Test conditionFromBool with false",
			args: args{b: false},
			want: v1.ConditionFalse,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := conditionFromBool(tt.args.b); got != tt.want {
				t.Errorf("conditionFromBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_notFromBool(t *testing.T) {
	type args struct {
		b bool
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "Test notFromBool with true",
			args: args{b: true},
			want: "",
		},
		{
			name: "Test notFromBool with false",
			args: args{b: false},
			want: "not ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notFromBool(tt.args.b); got != tt.want {
				t.Errorf("notFromBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_trimAndCapitalize(t *testing.T) {
	type args struct {
		s string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "Test trimAndCapitalize with empty string",
			args: args{s: ""},
			want: "",
		},
		{
			name: "Test trimAndCapitalize with single character",
			args: args{s: "a"},
			want: "A",
		},
		{
			name: "Test trimAndCapitalize with multiple characters",
			args: args{s: "abc"},
			want: "Abc",
		},
		{
			name: "Test trimAndCapitalize with multiple characters and spaces",
			args: args{s: "  ab c  "},
			want: "Ab c",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimAndCapitalize(tt.args.s); got != tt.want {
				t.Errorf("trimAndCapitalize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClusterPodPlacementConfigStatus_Build(t *testing.T) {
	tests := []struct {
		name                                           string
		podPlacementControllerAvailable                bool
		podPlacementWebhookAvailable                   bool
		podPlacementControllerUpToDate                 bool
		podPlacementWebhookUpToDate                    bool
		mutatingWebhookConfigurationAvailable          bool
		deprovisioning                                 bool
		expectDegraded                                 bool
		expectDeprovisioning                           bool
		expectMutatingWebhookConfigurationNotAvailable bool
		expectPodPlacementControllerNotReady           bool
		expectPodPlacementWebhookNotReady              bool
		expectAvailable                                bool
		expectProgressing                              bool
		expectCanDeployMutatingWebhook                 bool
	}{
		{
			name:                                           "Deprovisioning",
			podPlacementControllerAvailable:                true,
			podPlacementWebhookAvailable:                   true,
			podPlacementControllerUpToDate:                 true,
			podPlacementWebhookUpToDate:                    true,
			mutatingWebhookConfigurationAvailable:          true,
			deprovisioning:                                 true,
			expectDegraded:                                 false,
			expectDeprovisioning:                           true,
			expectMutatingWebhookConfigurationNotAvailable: false,
			expectPodPlacementControllerNotReady:           false,
			expectPodPlacementWebhookNotReady:              false,
			expectAvailable:                                true,
			expectProgressing:                              false,
			expectCanDeployMutatingWebhook:                 false,
		},
		{
			name:                                           "AllAvailableAndUpToDate",
			podPlacementControllerAvailable:                true,
			podPlacementWebhookAvailable:                   true,
			podPlacementControllerUpToDate:                 true,
			podPlacementWebhookUpToDate:                    true,
			mutatingWebhookConfigurationAvailable:          true,
			deprovisioning:                                 false,
			expectDegraded:                                 false,
			expectDeprovisioning:                           false,
			expectMutatingWebhookConfigurationNotAvailable: false,
			expectPodPlacementControllerNotReady:           false,
			expectPodPlacementWebhookNotReady:              false,
			expectAvailable:                                true,
			expectProgressing:                              false,
			expectCanDeployMutatingWebhook:                 true,
		},
		{
			name:                                           "MutatingWebhookConfigurationNotAvailable",
			podPlacementControllerAvailable:                true,
			podPlacementWebhookAvailable:                   true,
			podPlacementControllerUpToDate:                 true,
			podPlacementWebhookUpToDate:                    true,
			mutatingWebhookConfigurationAvailable:          false,
			deprovisioning:                                 false,
			expectDegraded:                                 true,
			expectDeprovisioning:                           false,
			expectMutatingWebhookConfigurationNotAvailable: true,
			expectPodPlacementControllerNotReady:           false,
			expectPodPlacementWebhookNotReady:              false,
			expectAvailable:                                false,
			expectProgressing:                              true,
			expectCanDeployMutatingWebhook:                 true,
		},
		{
			name:                                           "PodPlacementControllerNotAvailable",
			podPlacementControllerAvailable:                false,
			podPlacementWebhookAvailable:                   true,
			podPlacementControllerUpToDate:                 false,
			podPlacementWebhookUpToDate:                    true,
			mutatingWebhookConfigurationAvailable:          true,
			deprovisioning:                                 false,
			expectDegraded:                                 true,
			expectDeprovisioning:                           false,
			expectMutatingWebhookConfigurationNotAvailable: false,
			expectPodPlacementControllerNotReady:           true,
			expectPodPlacementWebhookNotReady:              false,
			expectAvailable:                                false,
			expectProgressing:                              true,
			expectCanDeployMutatingWebhook:                 false,
		},
		{
			name:                                           "PodPlacementWebhookNotUpToDate",
			podPlacementControllerAvailable:                true,
			podPlacementWebhookAvailable:                   true,
			podPlacementControllerUpToDate:                 true,
			podPlacementWebhookUpToDate:                    false,
			mutatingWebhookConfigurationAvailable:          true,
			deprovisioning:                                 false,
			expectDegraded:                                 false,
			expectDeprovisioning:                           false,
			expectMutatingWebhookConfigurationNotAvailable: false,
			expectPodPlacementControllerNotReady:           false,
			expectPodPlacementWebhookNotReady:              true,
			expectAvailable:                                true,
			expectProgressing:                              true,
			expectCanDeployMutatingWebhook:                 true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &ClusterPodPlacementConfigStatus{}
			s.Build(
				tt.podPlacementControllerAvailable,
				tt.podPlacementWebhookAvailable,
				tt.podPlacementControllerUpToDate,
				tt.podPlacementWebhookUpToDate,
				tt.mutatingWebhookConfigurationAvailable,
				tt.deprovisioning,
			)

			if s.degraded != tt.expectDegraded {
				t.Errorf("degraded = %v, expected %v", s.degraded, tt.expectDegraded)
			}
			if s.deprovisioning != tt.expectDeprovisioning {
				t.Errorf("deprovisioning = %v, expected %v", s.deprovisioning, tt.expectDeprovisioning)
			}
			if s.mutatingWebhookConfigurationNotAvailable != tt.expectMutatingWebhookConfigurationNotAvailable {
				t.Errorf("mutatingWebhookConfigurationNotAvailable = %v, expected %v", s.mutatingWebhookConfigurationNotAvailable, tt.expectMutatingWebhookConfigurationNotAvailable)
			}
			if s.podPlacementControllerNotReady != tt.expectPodPlacementControllerNotReady {
				t.Errorf("podPlacementControllerNotReady = %v, expected %v", s.podPlacementControllerNotReady, tt.expectPodPlacementControllerNotReady)
			}
			if s.podPlacementWebhookNotReady != tt.expectPodPlacementWebhookNotReady {
				t.Errorf("podPlacementWebhookNotReady = %v, expected %v", s.podPlacementWebhookNotReady, tt.expectPodPlacementWebhookNotReady)
			}
			if s.available != tt.expectAvailable {
				t.Errorf("available = %v, expected %v", s.available, tt.expectAvailable)
			}
			if s.progressing != tt.expectProgressing {
				t.Errorf("progressing = %v, expected %v", s.progressing, tt.expectProgressing)
			}
			if s.canDeployMutatingWebhook != tt.expectCanDeployMutatingWebhook {
				t.Errorf("canDeployMutatingWebhook = %v, expected %v", s.canDeployMutatingWebhook, tt.expectCanDeployMutatingWebhook)
			}
		})
	}
}
