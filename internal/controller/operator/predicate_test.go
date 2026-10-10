package operator

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	. "github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
)

func TestCppcUpdatePredicate(t *testing.T) {
	now := metav1.Now()
	p := cppcUpdatePredicate()

	tests := []struct {
		name string
		old  *multiarchv1beta1.ClusterPodPlacementConfig
		new  *multiarchv1beta1.ClusterPodPlacementConfig
		want bool
	}{
		{
			name: "generation change passes",
			old:  NewClusterPodPlacementConfig().WithGeneration(1).Build(),
			new:  NewClusterPodPlacementConfig().WithGeneration(2).Build(),
			want: true,
		},
		{
			name: "deletionTimestamp change passes",
			old:  NewClusterPodPlacementConfig().WithGeneration(1).Build(),
			new:  NewClusterPodPlacementConfig().WithGeneration(1).WithDeletionTimestamp(&now).Build(),
			want: true,
		},
		{
			name: "finalizer change passes",
			old:  NewClusterPodPlacementConfig().WithGeneration(1).Build(),
			new:  NewClusterPodPlacementConfig().WithGeneration(1).WithFinalizers("cleanup").Build(),
			want: true,
		},
		{
			name: "status-only update filtered",
			old:  NewClusterPodPlacementConfig().WithGeneration(1).Build(),
			new: NewClusterPodPlacementConfig().WithGeneration(1).
				WithStatusCondition("Available", metav1.ConditionTrue, "Ready", now).Build(),
			want: false,
		},
		{
			name: "label-only update filtered",
			old:  NewClusterPodPlacementConfig().WithGeneration(1).Build(),
			new:  NewClusterPodPlacementConfig().WithGeneration(1).WithLabels(map[string]string{"foo": "bar"}).Build(),
			want: false,
		},
		{
			name: "annotation-only update filtered",
			old:  NewClusterPodPlacementConfig().WithGeneration(1).Build(),
			new:  NewClusterPodPlacementConfig().WithGeneration(1).WithAnnotations(map[string]string{"note": "test"}).Build(),
			want: false,
		},
		{
			name: "no change filtered",
			old:  NewClusterPodPlacementConfig().WithGeneration(1).WithFinalizers("cleanup").Build(),
			new:  NewClusterPodPlacementConfig().WithGeneration(1).WithFinalizers("cleanup").Build(),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.Update(event.UpdateEvent{
				ObjectOld: tt.old,
				ObjectNew: tt.new,
			})
			if got != tt.want {
				t.Errorf("UpdateFunc() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCppcUpdatePredicate_CreateDeleteGenericPassThrough(t *testing.T) {
	p := cppcUpdatePredicate()
	now := metav1.Now()
	obj := NewClusterPodPlacementConfig().WithName("cluster").WithDeletionTimestamp(&now).Build()

	if !p.Create(event.CreateEvent{Object: obj}) {
		t.Error("CreateFunc should always return true")
	}
	if !p.Delete(event.DeleteEvent{Object: obj}) {
		t.Error("DeleteFunc should always return true")
	}
	if !p.Generic(event.GenericEvent{Object: obj}) {
		t.Error("GenericFunc should always return true")
	}
}

func TestCppcUpdatePredicate_BothTimestampsSet(t *testing.T) {
	p := cppcUpdatePredicate()
	t1 := metav1.NewTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	t2 := metav1.NewTime(time.Date(2025, 1, 1, 0, 0, 1, 0, time.UTC))

	got := p.Update(event.UpdateEvent{
		ObjectOld: NewClusterPodPlacementConfig().WithGeneration(1).WithDeletionTimestamp(&t1).Build(),
		ObjectNew: NewClusterPodPlacementConfig().WithGeneration(1).WithDeletionTimestamp(&t2).Build(),
	})
	if !got {
		t.Error("different deletionTimestamps should pass")
	}
}
