package operator

import (
	"context"
	"errors"
	"fmt"
	"testing"

	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/source"

	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
)

// fakeController records the sources passed to Watch instead of starting them. Watch fails while fail is true.
// Any other method panics.
type fakeController struct {
	controller.Controller
	watched []string
	fail    bool
}

func (c *fakeController) Watch(src source.Source) error {
	if c.fail {
		return errors.New("watch registration failed")
	}
	c.watched = append(c.watched, fmt.Sprint(src))
	return nil
}

type availabilityResult struct {
	available bool
	err       error
}

// newMonitoringTestReconciler returns a reconciler whose monitoring availability checks return, in order, the given
// results, and whose watches are recorded by a fake controller. Once the results are exhausted, the last one is
// returned for any further check. The default monitoringAvailableFunc is restored at the end of the test.
func newMonitoringTestReconciler(t *testing.T, results ...availabilityResult) (*ClusterPodPlacementConfigReconciler, *fakeController) {
	t.Helper()
	checks := 0
	defaultFunc := monitoringAvailableFunc
	monitoringAvailableFunc = func(context.Context, *ClusterPodPlacementConfigReconciler) (bool, error) {
		res := results[min(checks, len(results)-1)]
		checks++
		return res.available, res.err
	}
	t.Cleanup(func() { monitoringAvailableFunc = defaultFunc })

	// The scheme must know the owner type, and the client provide a REST mapper, as watchOwned builds an
	// owner-based event handler.
	scheme := runtime.NewScheme()
	if err := multiarchv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	ctrl := &fakeController{}
	return &ClusterPodPlacementConfigReconciler{
		Client:     fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme:     scheme,
		controller: ctrl,
	}, ctrl
}

var monitoringWatchSources = []string{"kind source: *v1.ServiceMonitor", "kind source: *v1.PrometheusRule"}

func TestEnsureMonitoring_RecoversAfterTransientError(t *testing.T) {
	g := NewWithT(t)
	transient := errors.New("service unavailable")
	r, ctrl := newMonitoringTestReconciler(t,
		availabilityResult{err: transient},
		availabilityResult{available: true},
	)

	available, err := r.ensureMonitoring(context.Background())
	g.Expect(err).To(MatchError(transient))
	g.Expect(available).To(BeFalse(), "monitoring objects must not be created while availability is unknown")
	g.Expect(ctrl.watched).To(BeEmpty(), "no watch should be registered while availability is unknown")

	available, err = r.ensureMonitoring(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(available).To(BeTrue())
	g.Expect(ctrl.watched).To(ConsistOf(monitoringWatchSources),
		"recovery must register the watches on the monitoring objects, not only create them")

	available, err = r.ensureMonitoring(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(available).To(BeTrue())
	g.Expect(ctrl.watched).To(HaveLen(len(monitoringWatchSources)), "the watches should be registered only once")
}

func TestEnsureMonitoring_NotAvailable(t *testing.T) {
	g := NewWithT(t)
	r, ctrl := newMonitoringTestReconciler(t, availabilityResult{available: false})

	available, err := r.ensureMonitoring(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(available).To(BeFalse())
	g.Expect(ctrl.watched).To(BeEmpty())
}

func TestEnsureMonitoring_WatchRegistrationFailure(t *testing.T) {
	g := NewWithT(t)
	r, ctrl := newMonitoringTestReconciler(t, availabilityResult{available: true})

	ctrl.fail = true
	available, err := r.ensureMonitoring(context.Background())
	g.Expect(err).To(HaveOccurred())
	g.Expect(available).To(BeFalse(), "monitoring objects must not be created if they cannot be watched")

	ctrl.fail = false
	available, err = r.ensureMonitoring(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(available).To(BeTrue())
	g.Expect(ctrl.watched).To(ConsistOf(monitoringWatchSources))
}

func TestEnsureMonitoring_ControllerNotSetUp(t *testing.T) {
	g := NewWithT(t)
	r, _ := newMonitoringTestReconciler(t, availabilityResult{available: true})
	r.controller = nil // as for a reconciler on which SetupWithManager was not called

	available, err := r.ensureMonitoring(context.Background())
	g.Expect(err).To(HaveOccurred(), "a missing controller should be reported as an error, not a panic")
	g.Expect(available).To(BeFalse())
}
