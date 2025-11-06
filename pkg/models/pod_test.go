package models

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

var ctx context.Context

func init() {
	ctx = context.TODO()
}

func TestPod_AddGate(t *testing.T) {
	type fields struct {
		Pod *v1.Pod
	}
	type args struct {
		gateName string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{
			name: "pod with no scheduling gates",
			fields: fields{
				Pod: builder.NewPod().Build(),
			},
			args: args{
				gateName: "test-gate",
			},
		},
		{
			name: "pod with empty scheduling gates",
			fields: fields{
				Pod: builder.NewPod().WithSchedulingGates().Build(),
			},
			args: args{
				gateName: "test-gate",
			},
		},
		{
			name: "pod with scheduling gates and no matching gate",
			fields: fields{
				Pod: builder.NewPod().WithSchedulingGates("other-gate").Build(),
			},
			args: args{
				gateName: "test-gate",
			},
		},
		{
			name: "pod with scheduling gates and matching gate",
			fields: fields{
				Pod: builder.NewPod().WithSchedulingGates("test-gate", "other-gate").Build(),
			},
			args: args{
				gateName: "test-gate",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := NewPod(tt.fields.Pod, ctx, nil)
			pod.AddGate(tt.args.gateName)
			if !pod.HasGate(tt.args.gateName) {
				t.Errorf("AddGate() did not add gate %s to pod", tt.args.gateName)
			}
		})
	}
}

func TestPod_HasGate(t *testing.T) {
	type fields struct {
		Pod      *v1.Pod
		ctx      context.Context
		recorder record.EventRecorder
	}
	type args struct {
		gateName string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "pod with no scheduling gates",
			fields: fields{
				Pod: builder.NewPod().Build(),
			},
			args: args{
				gateName: "test-gate",
			},
			want: false,
		},
		{
			name: "pod with empty scheduling gates",
			fields: fields{
				Pod: builder.NewPod().WithSchedulingGates().Build(),
			},
			args: args{
				gateName: "test-gate",
			},
			want: false,
		},
		{
			name: "pod with scheduling gates and no matching gate",
			fields: fields{
				Pod: builder.NewPod().WithSchedulingGates("other-gate").Build(),
			},
			args: args{
				gateName: "test-gate",
			},
			want: false,
		},
		{
			name: "pod with scheduling gates and matching gate",
			fields: fields{
				Pod: builder.NewPod().WithSchedulingGates("test-gate").Build(),
			},
			args: args{
				gateName: "test-gate",
			},
			want: true,
		},
		{
			name: "pod with multiple scheduling gates and matching gate",
			fields: fields{
				Pod: builder.NewPod().WithSchedulingGates("other-gate", "test-gate", "another-gate").Build(),
			},
			args: args{
				gateName: "test-gate",
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := NewPod(tt.fields.Pod, tt.fields.ctx, tt.fields.recorder)
			if got := pod.HasGate(tt.args.gateName); got != tt.want {
				t.Errorf("HasGate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPod_RemoveGate(t *testing.T) {
	type fields struct {
		Pod      *v1.Pod
		ctx      context.Context
		recorder record.EventRecorder
	}
	type args struct {
		gateName string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{
			name: "pod with no scheduling gates",
			fields: fields{
				Pod: builder.NewPod().Build(),
			},
			args: args{
				gateName: "test-gate",
			},
		},
		{
			name: "pod with non-matching scheduling gates",
			fields: fields{
				Pod: builder.NewPod().WithSchedulingGates("other-gate").Build(),
			},
			args: args{
				gateName: "test-gate",
			},
		},
		{
			name: "pod with matching scheduling gate",
			fields: fields{
				Pod: builder.NewPod().WithSchedulingGates("test-gate").Build(),
			},
			args: args{
				gateName: "test-gate",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := NewPod(tt.fields.Pod, tt.fields.ctx, tt.fields.recorder)
			pod.RemoveGate(tt.args.gateName)
			if got := pod.HasGate(tt.args.gateName); got {
				t.Errorf("RemoveGate() = true, want false")
			}
		})
	}
}

func TestEnsureLabel(t *testing.T) {
	tests := []struct {
		name           string
		initialLabels  []string
		label          string
		value          string
		expectedLabels map[string]string
	}{
		{
			name:           "Empty Labels",
			initialLabels:  nil,
			label:          "testLabel",
			value:          "testValue",
			expectedLabels: map[string]string{"testLabel": "testValue"},
		},
		{
			name:           "Non-empty Labels",
			initialLabels:  []string{"existingLabel", "existingValue"},
			label:          "testLabel",
			value:          "testValue",
			expectedLabels: map[string]string{"existingLabel": "existingValue", "testLabel": "testValue"},
		},
		{
			name:           "Overwrite Existing Label",
			initialLabels:  []string{"testLabel", "oldValue"},
			label:          "testLabel",
			value:          "newValue",
			expectedLabels: map[string]string{"testLabel": "newValue"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := NewPod(builder.NewPod().WithLabels(tt.initialLabels...).Build(), ctx, nil)
			pod.EnsureLabel(tt.label, tt.value)

			if len(pod.Labels) != len(tt.expectedLabels) {
				t.Errorf("expected %d labels, got %d", len(tt.expectedLabels), len(pod.Labels))
			}

			for k, v := range tt.expectedLabels {
				if pod.Labels[k] != v {
					t.Errorf("expected label %s to have value %s, got %s", k, v, pod.Labels[k])
				}
			}
		})
	}
}

func TestPod_EnsureAnnotation(t *testing.T) {
	type args struct {
		annotation string
		value      string
	}
	tests := []struct {
		name               string
		initialAnnotations map[string]string
		args               args
	}{
		{
			name:               "pod with no annotations",
			initialAnnotations: nil,
			args: args{
				annotation: "test-annotation",
				value:      "test-value",
			},
		},
		{
			name: "pod with existing annotation",
			initialAnnotations: map[string]string{
				"existing-annotation": "existing-value",
			},
			args: args{
				annotation: "test-annotation",
				value:      "test-value",
			},
		},
		{
			name: "pod with multiple annotations including the one to update",
			initialAnnotations: map[string]string{
				"existing-annotation": "existing-value",
				"test-annotation":     "old-value",
			},
			args: args{
				annotation: "test-annotation",
				value:      "new-value",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := NewPod(builder.NewPod().WithAnnotations(tt.initialAnnotations).Build(), ctx, nil)
			pod.EnsureAnnotation(tt.args.annotation, tt.args.value)
			for k, v := range tt.initialAnnotations {
				if k == tt.args.annotation {
					if pod.Annotations[k] != tt.args.value {
						t.Errorf("EnsureAnnotation() did not update annotation %s, expected %s, got %s", k, tt.args.value, pod.Annotations[k])
					}
				} else {
					if pod.Annotations[k] != v {
						t.Errorf("EnsureAnnotation() removed annotation %s unexpectedly, expected %s, got %s", k, v, pod.Annotations[k])
					}
				}
			}
		})
	}
}

func TestPod_EnsureNoLabel(t *testing.T) {
	type args struct {
		label string
	}
	tests := []struct {
		name            string
		labelValuePairs []string
		args            args
	}{
		{
			name: "pod with no labels",
			args: args{
				label: "test-label",
			},
		},
		{
			name: "pod with existing label",
			labelValuePairs: []string{
				"test-label", "test-value",
			},
			args: args{
				label: "test-label",
			},
		},
		{
			name: "pod with multiple labels including the one to remove",
			labelValuePairs: []string{
				"test-label", "test-value",
				"another-label", "another-value",
			},
			args: args{
				label: "test-label",
			},
		},
		{
			name: "pod with multiple labels but not the one to remove",
			labelValuePairs: []string{
				"another-label", "another-value",
				"yet-another-label", "yet-another-value",
			},
			args: args{
				label: "test-label",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := NewPod(builder.NewPod().WithLabels(tt.labelValuePairs...).Build(), ctx, nil)
			pod.EnsureNoLabel(tt.args.label)
			for i, label := range tt.labelValuePairs {
				if i%2 != 0 {
					continue // Skip values, only check keys
				}
				if pl, ok := pod.Labels[label]; ok && label == tt.args.label {
					t.Errorf("EnsureNoLabel() did not remove label %s, found value %s", label, pl)
				} else if !ok && label != tt.args.label {
					t.Errorf("EnsureNoLabel() removed label %s unexpectedly", label)
				}
			}
		})
	}
}
func TestPod_HasControlPlaneNodeSelector(t *testing.T) {
	type fields struct {
		Pod      *v1.Pod
		ctx      context.Context
		recorder record.EventRecorder
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "pod with no node selector terms",
			fields: fields{
				Pod: builder.NewPod().Build(),
			},
			want: false,
		},
		{
			name: "pod with empty node selector terms",
			fields: fields{
				Pod: builder.NewPod().WithNodeSelectors().Build(),
			},
			want: false,
		},
		{
			name: "pod with node selector terms and no control plane node selector",
			fields: fields{
				Pod: builder.NewPod().WithNodeSelectors("foo", "bar").Build(),
			},
			want: false,
		},
		{
			name: "pod with node selector terms and control plane node selector",
			fields: fields{
				Pod: builder.NewPod().WithNodeSelectors("foo", "bar", utils.ControlPlaneNodeSelectorLabel, "").Build(),
			},
			want: true,
		},
		{
			name: "pod with node selector terms and control plane node selector and other node selector",
			fields: fields{
				Pod: builder.NewPod().WithNodeSelectors("foo", "bar", utils.MasterNodeSelectorLabel, "", "baz", "foo").Build(),
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := NewPod(tt.fields.Pod, tt.fields.ctx, tt.fields.recorder)
			if got := pod.HasControlPlaneNodeSelector(); got != tt.want {
				t.Errorf("hasControlPlaneNodeSelector() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPod_EnsureAndIncrementLabel(t *testing.T) {
	type fields struct {
		Pod      *v1.Pod
		ctx      context.Context
		recorder record.EventRecorder
	}
	type args struct {
		label string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   string
	}{
		{
			name: "pod with no labels",
			fields: fields{
				Pod: builder.NewPod().Build(),
			},
			args: args{
				label: "test-label",
			},
			want: "1",
		},
		{
			name: "pod with existing label",
			fields: fields{
				Pod: builder.NewPod().WithLabels("test-label", "2").Build(),
			},
			args: args{
				label: "test-label",
			},
			want: "3",
		},
		{
			name: "pod with existing label as non-numeric",
			fields: fields{
				Pod: builder.NewPod().WithLabels("test-label", "non-numeric").Build(),
			},
			args: args{
				label: "test-label",
			},
			want: "1", // Non-numeric value should reset to 1
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := NewPod(tt.fields.Pod, tt.fields.ctx, tt.fields.recorder)
			pod.EnsureAndIncrementLabel(tt.args.label)
			if value, exists := pod.Labels[tt.args.label]; exists {
				if value != tt.want {
					t.Errorf("EnsureAndIncrementLabel() = %v, want %v", value, tt.want)
				}
			} else {
				t.Errorf("EnsureAndIncrementLabel() did not set label %s", tt.args.label)
			}
		})
	}
}

func TestPod_IsFromDaemonSet(t *testing.T) {
	type fields struct {
		Pod      *v1.Pod
		ctx      context.Context
		recorder record.EventRecorder
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "pod with no owner references",
			fields: fields{
				Pod: builder.NewPod().Build(),
			},
			want: false,
		},
		{
			name: "pod with owner reference not a DaemonSet",
			fields: fields{
				Pod: builder.NewPod().WithOwnerReference(metav1.OwnerReference{
					Kind:       "Deployment",
					Name:       "test-deployment",
					Controller: utils.NewPtr(true),
				}).Build(),
			},
			want: false,
		},
		{
			name: "pod with owner reference as DaemonSet",
			fields: fields{
				Pod: builder.NewPod().WithOwnerReference(metav1.OwnerReference{
					Kind:       "DaemonSet",
					Name:       "test-daemonset",
					Controller: utils.NewPtr(true),
				}).Build(),
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := NewPod(tt.fields.Pod, tt.fields.ctx, tt.fields.recorder)
			if got := pod.IsFromDaemonSet(); got != tt.want {
				t.Errorf("IsFromDaemonSet() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPod_ContainerNameFor(t *testing.T) {
	type fields struct {
		Pod      v1.Pod
		ctx      context.Context
		recorder record.EventRecorder
	}
	type args struct {
		containerID string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "valid container ID matches a container (cri-o)",
			fields: fields{
				Pod: v1.Pod{
					Status: v1.PodStatus{
						ContainerStatuses: []v1.ContainerStatus{
							{
								Name:        "test-container",
								ContainerID: "cri-o://1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
							},
						},
					},
				},
			},
			args: args{
				containerID: "cri-o://1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			},
			want:    "test-container",
			wantErr: false,
		},
		{
			name: "valid container ID matches a container (containerd)",
			fields: fields{
				Pod: v1.Pod{
					Status: v1.PodStatus{
						ContainerStatuses: []v1.ContainerStatus{
							{
								Name:        "test-container",
								ContainerID: "containerd://abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
							},
						},
					},
				},
			},
			args: args{
				containerID: "containerd://abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			},
			want:    "test-container",
			wantErr: false,
		},
		{
			name: "valid container ID does not match any container",
			fields: fields{
				Pod: v1.Pod{
					Status: v1.PodStatus{
						ContainerStatuses: []v1.ContainerStatus{
							{
								Name:        "test-container",
								ContainerID: "cri-o://abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
							},
						},
					},
				},
			},
			args: args{
				containerID: "cri-o://1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "invalid container ID format",
			fields: fields{
				Pod: v1.Pod{
					Status: v1.PodStatus{
						ContainerStatuses: []v1.ContainerStatus{
							{
								Name:        "test-container",
								ContainerID: "cri-o://1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
							},
						},
					},
				},
			},
			args: args{
				containerID: "invalid-container-id",
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "empty container ID",
			fields: fields{
				Pod: v1.Pod{
					Status: v1.PodStatus{
						ContainerStatuses: []v1.ContainerStatus{
							{
								Name:        "test-container",
								ContainerID: "cri-o://1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
							},
						},
					},
				},
			},
			args: args{
				containerID: "",
			},
			want:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &Pod{
				Pod:      tt.fields.Pod,
				ctx:      tt.fields.ctx,
				recorder: tt.fields.recorder,
			}
			got, err := pod.ContainerNameFor(tt.args.containerID)
			if (err != nil) != tt.wantErr {
				t.Errorf("ContainerNameFor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ContainerNameFor() got = %v, want %v", got, tt.want)
			}
		})
	}
}
