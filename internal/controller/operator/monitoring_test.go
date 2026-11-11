package operator

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/cache/informertest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
)

// countingController wraps a real controller and counts the watches registered on it.
type countingController struct {
	controller.Controller
	watches atomic.Int32
}

func (c *countingController) Watch(src source.Source) error {
	c.watches.Add(1)
	return c.Controller.Watch(src)
}

type availabilityResult struct {
	available bool
	err       error
}

type monitoringTestEnv struct {
	r         *ClusterPodPlacementConfigReconciler
	ctrl      *countingController
	informers *informertest.FakeInformers
	// requests receives the requests reconciled by the running controller.
	requests chan reconcile.Request
}

// newMonitoringTestEnv returns a reconciler whose monitoring availability checks return, in order, the given
// results (the last one is returned for any further check), backed by a real, running controller whose watches are
// served by a fake informer cache, so that registered watches are actually started and synced, and events delivered.
// The default monitoringAvailableFunc is restored, and the controller stopped, at the end of the test.
func newMonitoringTestEnv(t *testing.T, results ...availabilityResult) *monitoringTestEnv {
	t.Helper()
	checks := 0
	defaultFunc := monitoringAvailableFunc
	monitoringAvailableFunc = func(context.Context, *ClusterPodPlacementConfigReconciler) (bool, error) {
		res := results[min(checks, len(results)-1)]
		checks++
		return res.available, res.err
	}
	t.Cleanup(func() { monitoringAvailableFunc = defaultFunc })

	scheme := runtime.NewScheme()
	if err := multiarchv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := monitoringv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	// The owner-based event handler maps the owner reference to a request using the REST mapper, which must know
	// that the ClusterPodPlacementConfig is cluster-scoped.
	mapper := meta.NewDefaultRESTMapper(nil)
	mapper.Add(multiarchv1beta1.GroupVersion.WithKind(multiarchv1beta1.ClusterPodPlacementConfigKind), meta.RESTScopeRoot)

	requests := make(chan reconcile.Request, 10)
	realCtrl, err := controller.NewUnmanaged("monitoring-test", controller.Options{
		Reconciler: reconcile.Func(func(_ context.Context, req reconcile.Request) (reconcile.Result, error) {
			requests <- req
			return reconcile.Result{}, nil
		}),
		SkipNameValidation: ptr.To(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Start the controller and wait until it is running, as ensureMonitoring is only called on a running controller:
	// the probe source is started by the controller while it holds its lock, and any Watch call made after that
	// blocks until the controller is running.
	started := make(chan struct{})
	if err := realCtrl.Watch(source.Func(
		func(context.Context, workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
			close(started)
			return nil
		})); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = realCtrl.Start(ctx) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the controller did not start")
	}

	informers := &informertest.FakeInformers{Scheme: scheme}
	ctrl := &countingController{Controller: realCtrl}
	return &monitoringTestEnv{
		r: &ClusterPodPlacementConfigReconciler{
			Client:     fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).Build(),
			Scheme:     scheme,
			controller: ctrl,
			cache:      informers,
		},
		ctrl:      ctrl,
		informers: informers,
		requests:  requests,
	}
}

// ownedByCPPC returns metadata for an object controlled by the ClusterPodPlacementConfig singleton.
func ownedByCPPC(name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: "test-namespace",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: multiarchv1beta1.GroupVersion.String(),
			Kind:       multiarchv1beta1.ClusterPodPlacementConfigKind,
			Name:       common.SingletonResourceObjectName,
			UID:        "cppc-uid",
			Controller: ptr.To(true),
		}},
	}
}

// expectEventEnqueuesOwner injects a creation event for obj in its fake informer and expects the running controller
// to reconcile the ClusterPodPlacementConfig owning it.
func (e *monitoringTestEnv) expectEventEnqueuesOwner(t *testing.T, obj client.Object) {
	t.Helper()
	g := NewWithT(t)
	informer, err := e.informers.FakeInformerFor(context.Background(), obj)
	g.Expect(err).NotTo(HaveOccurred())
	informer.Add(obj)
	g.Eventually(e.requests).WithTimeout(5*time.Second).Should(Receive(Equal(reconcile.Request{
		NamespacedName: types.NamespacedName{Name: common.SingletonResourceObjectName},
	})), "an event on a %T owned by the ClusterPodPlacementConfig should enqueue it", obj)
}

func TestEnsureMonitoring_RecoversAfterTransientError(t *testing.T) {
	g := NewWithT(t)
	transient := errors.New("service unavailable")
	env := newMonitoringTestEnv(t,
		availabilityResult{err: transient},
		availabilityResult{available: true},
	)

	available, err := env.r.ensureMonitoring(context.Background())
	g.Expect(err).To(MatchError(transient))
	g.Expect(available).To(BeFalse(), "monitoring objects must not be created while availability is unknown")
	g.Expect(env.ctrl.watches.Load()).To(BeZero(), "no watch should be registered while availability is unknown")

	available, err = env.r.ensureMonitoring(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(available).To(BeTrue())
	g.Expect(env.ctrl.watches.Load()).To(BeEquivalentTo(2))
	// The recovered watches are started and deliver events to the owner.
	env.expectEventEnqueuesOwner(t, &monitoringv1.ServiceMonitor{ObjectMeta: ownedByCPPC("service-monitor")})
	env.expectEventEnqueuesOwner(t, &monitoringv1.PrometheusRule{ObjectMeta: ownedByCPPC("prometheus-rule")})

	available, err = env.r.ensureMonitoring(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(available).To(BeTrue())
	g.Expect(env.ctrl.watches.Load()).To(BeEquivalentTo(2), "the watches should be registered only once")
}

func TestEnsureMonitoring_NotAvailable(t *testing.T) {
	g := NewWithT(t)
	env := newMonitoringTestEnv(t, availabilityResult{available: false})

	available, err := env.r.ensureMonitoring(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(available).To(BeFalse())
	g.Expect(env.ctrl.watches.Load()).To(BeZero())
}

func TestEnsureMonitoring_WatchDoesNotSync(t *testing.T) {
	g := NewWithT(t)
	env := newMonitoringTestEnv(t, availabilityResult{available: true})
	// The informer cache does not sync, as if the monitoring watches could not be started.
	synced := false
	env.informers.Synced = &synced

	available, err := env.r.ensureMonitoring(context.Background())
	g.Expect(err).To(HaveOccurred())
	g.Expect(available).To(BeFalse(), "monitoring objects must not be created if they cannot be watched")
	g.Expect(env.r.serviceMonitorsWatched).To(BeFalse(), "a watch that did not sync should not be considered registered")

	// The cache syncs again: the next attempt registers working watches.
	synced = true
	available, err = env.r.ensureMonitoring(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(available).To(BeTrue())
	env.expectEventEnqueuesOwner(t, &monitoringv1.ServiceMonitor{ObjectMeta: ownedByCPPC("service-monitor")})
	env.expectEventEnqueuesOwner(t, &monitoringv1.PrometheusRule{ObjectMeta: ownedByCPPC("prometheus-rule")})
}

func TestEnsureMonitoring_WatchSyncTimeout(t *testing.T) {
	g := NewWithT(t)
	defaultTimeout := monitoringWatchSyncTimeout
	monitoringWatchSyncTimeout = 100 * time.Millisecond
	t.Cleanup(func() { monitoringWatchSyncTimeout = defaultTimeout })
	env := newMonitoringTestEnv(t, availabilityResult{available: true})
	// The informer for the monitoring resources cannot be obtained, as if their CRDs were not installed:
	// the watch keeps retrying and never syncs.
	env.informers.Error = errors.New("no matches for kind")

	available, err := env.r.ensureMonitoring(context.Background())
	g.Expect(err).To(HaveOccurred())
	g.Expect(available).To(BeFalse(), "monitoring objects must not be created if they cannot be watched")
	g.Expect(env.r.serviceMonitorsWatched).To(BeFalse(), "a watch that timed out should not be considered registered")
}

func TestEnsureMonitoring_ControllerNotSetUp(t *testing.T) {
	g := NewWithT(t)
	env := newMonitoringTestEnv(t, availabilityResult{available: true})
	env.r.controller = nil // as for a reconciler on which SetupWithManager was not called

	available, err := env.r.ensureMonitoring(context.Background())
	g.Expect(err).To(HaveOccurred(), "a missing controller should be reported as an error, not a panic")
	g.Expect(available).To(BeFalse())
}
