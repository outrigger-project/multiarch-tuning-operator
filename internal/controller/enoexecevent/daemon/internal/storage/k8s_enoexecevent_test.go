package storage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/time/rate"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	storagetypes "github.com/openshift/multiarch-tuning-operator/internal/controller/enoexecevent/daemon/internal/types"
)

// fakeStatusWriter wraps a real SubResourceWriter and injects conflict errors
// for a configurable number of calls before delegating to the underlying writer.
type fakeStatusWriter struct {
	delegate      client.SubResourceWriter
	conflictsLeft atomic.Int32
	conflictsSeen atomic.Int32
}

func (f *fakeStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return f.delegate.Create(ctx, obj, subResource, opts...)
}

func (f *fakeStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	if f.conflictsLeft.Add(-1) >= 0 {
		f.conflictsSeen.Add(1)
		return apierrors.NewConflict(
			schema.GroupResource{Group: "multiarch.openshift.io", Resource: "enoexecevents"},
			obj.GetName(),
			nil,
		)
	}
	return f.delegate.Update(ctx, obj, opts...)
}

func (f *fakeStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return f.delegate.Patch(ctx, obj, patch, opts...)
}

func (f *fakeStatusWriter) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
	return f.delegate.Apply(ctx, obj, opts...)
}

// fakeClient wraps a real client.Client and overrides Status() to return
// a fakeStatusWriter that can inject conflict errors.
type fakeClient struct {
	client.Client
	statusWriter *fakeStatusWriter
}

func (f *fakeClient) Status() client.SubResourceWriter {
	return f.statusWriter
}

// simpleObjectStore is a minimal in-memory store for ENoExecEvent objects,
// implementing the subset of client.Client needed by processEvent.
type simpleObjectStore struct {
	objects map[types.NamespacedName]*multiarchv1beta1.ENoExecEvent
}

func newSimpleObjectStore() *simpleObjectStore {
	return &simpleObjectStore{objects: make(map[types.NamespacedName]*multiarchv1beta1.ENoExecEvent)}
}

type simpleClient struct {
	client.Client // embed to satisfy interface; unused methods will panic
	store         *simpleObjectStore
	scheme        *runtime.Scheme
}

func (c *simpleClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	enee, ok := obj.(*multiarchv1beta1.ENoExecEvent)
	if !ok {
		return apierrors.NewBadRequest("only ENoExecEvent supported")
	}
	key := types.NamespacedName{Name: enee.Name, Namespace: enee.Namespace}
	if _, exists := c.store.objects[key]; exists {
		return apierrors.NewAlreadyExists(schema.GroupResource{}, enee.Name)
	}
	stored := enee.DeepCopy()
	stored.ResourceVersion = "1"
	// Server strips status on create
	stored.Status = multiarchv1beta1.ENoExecEventStatus{}
	c.store.objects[key] = stored
	// Write back to caller (like a real API server does)
	stored.DeepCopyInto(enee)
	return nil
}

func (c *simpleClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	enee, ok := obj.(*multiarchv1beta1.ENoExecEvent)
	if !ok {
		return apierrors.NewBadRequest("only ENoExecEvent supported")
	}
	stored, exists := c.store.objects[types.NamespacedName(key)]
	if !exists {
		return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
	}
	stored.DeepCopyInto(enee)
	return nil
}

func (c *simpleClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	key := types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
	delete(c.store.objects, key)
	return nil
}

func (c *simpleClient) Scheme() *runtime.Scheme {
	return c.scheme
}

func (c *simpleClient) Status() client.SubResourceWriter {
	return &simpleStatusWriter{store: c.store}
}

type simpleStatusWriter struct {
	store *simpleObjectStore
}

func (w *simpleStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return nil
}

func (w *simpleStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	enee, ok := obj.(*multiarchv1beta1.ENoExecEvent)
	if !ok {
		return apierrors.NewBadRequest("only ENoExecEvent supported")
	}
	key := types.NamespacedName{Name: enee.Name, Namespace: enee.Namespace}
	stored, exists := w.store.objects[key]
	if !exists {
		return apierrors.NewNotFound(schema.GroupResource{}, enee.Name)
	}
	stored.Status = enee.Status
	return nil
}

func (w *simpleStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return nil
}

func (w *simpleStatusWriter) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
	return nil
}

func newTestStorage(t *testing.T, k8sClient client.Client) *K8sENOExecEventStorage {
	t.Helper()
	ctx := logr.NewContext(context.Background(), logr.Discard())
	return &K8sENOExecEventStorage{
		IWStorageBase: &IWStorageBase{
			ctx: ctx,
			ch:  make(chan *storagetypes.ENOEXECInternalEvent, 10),
		},
		nodeName:  "test-node",
		namespace: "test-namespace",
		limiter:   rate.NewLimiter(rate.Inf, 1),
		timeout:   10 * time.Second,
		k8sClient: k8sClient,
	}
}

func TestProcessEvent_Success(t *testing.T) {
	store := newSimpleObjectStore()
	base := &simpleClient{store: store, scheme: runtime.NewScheme()}
	storage := newTestStorage(t, base)

	event := &storagetypes.ENOEXECInternalEvent{
		PodName:      "test-pod",
		PodNamespace: "test-ns",
		ContainerID:  "abc123",
	}

	err := storage.processEvent(event)
	if err != nil {
		t.Fatalf("processEvent should succeed, got: %v", err)
	}

	// Verify the object was created with status set
	if len(store.objects) != 1 {
		t.Fatalf("expected 1 object in store, got %d", len(store.objects))
	}
	for _, obj := range store.objects {
		if obj.Status.PodName != "test-pod" {
			t.Errorf("expected PodName 'test-pod', got %q", obj.Status.PodName)
		}
		if obj.Status.PodNamespace != "test-ns" {
			t.Errorf("expected PodNamespace 'test-ns', got %q", obj.Status.PodNamespace)
		}
		if obj.Status.ContainerID != "abc123" {
			t.Errorf("expected ContainerID 'abc123', got %q", obj.Status.ContainerID)
		}
		if obj.Status.NodeName != "test-node" {
			t.Errorf("expected NodeName 'test-node', got %q", obj.Status.NodeName)
		}
	}
}

func TestProcessEvent_ConflictRetrySucceeds(t *testing.T) {
	store := newSimpleObjectStore()
	base := &simpleClient{store: store, scheme: runtime.NewScheme()}
	statusWriter := &fakeStatusWriter{
		delegate: base.Status(),
	}
	statusWriter.conflictsLeft.Store(2) // fail first 2 attempts, succeed on 3rd
	wrappedClient := &fakeClient{Client: base, statusWriter: statusWriter}
	storage := newTestStorage(t, wrappedClient)

	event := &storagetypes.ENOEXECInternalEvent{
		PodName:      "test-pod",
		PodNamespace: "test-ns",
		ContainerID:  "abc123",
	}

	err := storage.processEvent(event)
	if err != nil {
		t.Fatalf("processEvent should succeed after retries, got: %v", err)
	}

	if statusWriter.conflictsSeen.Load() != 2 {
		t.Errorf("expected 2 conflict retries, got %d", statusWriter.conflictsSeen.Load())
	}

	// Verify the object was created with status set
	if len(store.objects) != 1 {
		t.Fatalf("expected 1 object in store, got %d", len(store.objects))
	}
	for _, obj := range store.objects {
		if obj.Status.PodName != "test-pod" {
			t.Errorf("expected PodName 'test-pod', got %q", obj.Status.PodName)
		}
	}
}

func TestProcessEvent_ConflictExhaustsRetries(t *testing.T) {
	store := newSimpleObjectStore()
	base := &simpleClient{store: store, scheme: runtime.NewScheme()}
	statusWriter := &fakeStatusWriter{
		delegate: base.Status(),
	}
	statusWriter.conflictsLeft.Store(10) // always conflict
	wrappedClient := &fakeClient{Client: base, statusWriter: statusWriter}
	storage := newTestStorage(t, wrappedClient)

	event := &storagetypes.ENOEXECInternalEvent{
		PodName:      "test-pod",
		PodNamespace: "test-ns",
		ContainerID:  "abc123",
	}

	err := storage.processEvent(event)
	if err == nil {
		t.Fatal("processEvent should fail when all retries are exhausted")
	}

	// retry.DefaultRetry has Steps=5, so 5 total attempts
	if statusWriter.conflictsSeen.Load() != 5 {
		t.Errorf("expected 5 conflict attempts, got %d", statusWriter.conflictsSeen.Load())
	}

	// Object should be rolled back (deleted)
	if len(store.objects) != 0 {
		t.Errorf("expected object to be rolled back (deleted), got %d objects", len(store.objects))
	}
}

func TestProcessEvent_NonConflictErrorNoRetry(t *testing.T) {
	store := newSimpleObjectStore()
	base := &simpleClient{store: store, scheme: runtime.NewScheme()}

	// Use a client that returns a non-conflict error on status update
	notFoundWriter := &notFoundStatusWriter{}
	wrappedClient := &nonConflictFakeClient{Client: base, statusWriter: notFoundWriter}
	storage := newTestStorage(t, wrappedClient)

	event := &storagetypes.ENOEXECInternalEvent{
		PodName:      "test-pod",
		PodNamespace: "test-ns",
		ContainerID:  "abc123",
	}

	err := storage.processEvent(event)
	if err == nil {
		t.Fatal("processEvent should fail on non-conflict error")
	}

	// Should have attempted only once (no retries for non-conflict errors)
	if notFoundWriter.callCount.Load() != 1 {
		t.Errorf("expected 1 attempt (no retry for non-conflict), got %d", notFoundWriter.callCount.Load())
	}

	// Object should be rolled back
	if len(store.objects) != 0 {
		t.Errorf("expected object to be rolled back, got %d objects", len(store.objects))
	}
}

type nonConflictFakeClient struct {
	client.Client
	statusWriter *notFoundStatusWriter
}

func (f *nonConflictFakeClient) Status() client.SubResourceWriter {
	return f.statusWriter
}

type notFoundStatusWriter struct {
	callCount atomic.Int32
}

func (w *notFoundStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return nil
}

func (w *notFoundStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	w.callCount.Add(1)
	return apierrors.NewNotFound(schema.GroupResource{}, obj.GetName())
}

func (w *notFoundStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return nil
}

func (w *notFoundStatusWriter) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
	return nil
}

// Verify fakeClient satisfies client.Client at compile time
var _ client.Client = &fakeClient{}

// Verify fakeStatusWriter satisfies SubResourceWriter at compile time
var _ client.SubResourceWriter = &fakeStatusWriter{}
var _ client.SubResourceWriter = &notFoundStatusWriter{}

// Verify simpleClient minimum interface needed by processEvent
var _ = (*simpleClient)(nil)
