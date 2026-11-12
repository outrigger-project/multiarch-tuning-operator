package utils

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

var testGVR = schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}

// resetAvailableResourcesCache clears the resource availability cache between test cases.
func resetAvailableResourcesCache() {
	rwMutex.Lock()
	defer rwMutex.Unlock()
	availableResourcesMap = map[schema.GroupVersionResource]bool{}
}

// newFakeDynamicClient returns a fake dynamic client whose List calls on testGVR return, in order, the given errors
// (nil meaning success). Once the errors are exhausted, the last one is returned for any further call.
// The returned counter reports how many List calls reached the client.
func newFakeDynamicClient(errs ...error) (*dynamicfake.FakeDynamicClient, *int) {
	calls := 0
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{testGVR: "ServiceMonitorList"})
	client.PrependReactor("list", testGVR.Resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
		err := errs[min(calls, len(errs)-1)]
		calls++
		return err != nil, nil, err
	})
	return client, &calls
}

func connectionRefusedError() error {
	return &net.OpError{Op: "dial", Net: "tcp",
		Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}
}

func Test_IsResourceAvailable(t *testing.T) {
	gr := testGVR.GroupResource()
	tests := []struct {
		name          string
		err           error
		wantAvailable bool
		wantErr       bool
		wantCached    bool
	}{
		{
			name:          "available resource is cached as available",
			err:           nil,
			wantAvailable: true,
			wantCached:    true,
		},
		{
			name:          "NotFound is cached as not available",
			err:           apierrors.NewNotFound(gr, ""),
			wantAvailable: false,
			wantCached:    true,
		},
		{
			name:          "MethodNotSupported is cached as not available",
			err:           apierrors.NewMethodNotSupported(gr, "list"),
			wantAvailable: false,
			wantCached:    true,
		},
		{
			name:          "NoMatch is cached as not available",
			err:           &meta.NoResourceMatchError{PartialResource: testGVR},
			wantAvailable: false,
			wantCached:    true,
		},
		{
			name:    "ServiceUnavailable is returned and not cached",
			err:     apierrors.NewServiceUnavailable("apiserver not ready"),
			wantErr: true,
		},
		{
			name:    "Timeout is returned and not cached",
			err:     apierrors.NewTimeoutError("timed out", 1),
			wantErr: true,
		},
		{
			name:    "TooManyRequests is returned and not cached",
			err:     apierrors.NewTooManyRequests("throttled", 1),
			wantErr: true,
		},
		{
			name:    "InternalError is returned and not cached",
			err:     apierrors.NewInternalError(errors.New("etcd unavailable")),
			wantErr: true,
		},
		{
			name:    "Forbidden is returned and not cached",
			err:     apierrors.NewForbidden(gr, "", errors.New("RBAC not yet propagated")),
			wantErr: true,
		},
		{
			name:    "connection refused is returned and not cached",
			err:     connectionRefusedError(),
			wantErr: true,
		},
		{
			name:    "context deadline exceeded is returned and not cached",
			err:     context.DeadlineExceeded,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			resetAvailableResourcesCache()
			// The second List call always succeeds: it is only reached if the first result was not cached.
			client, calls := newFakeDynamicClient(tt.err, nil)

			available, err := IsResourceAvailable(context.Background(), client, testGVR)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				g.Expect(errors.Is(err, tt.err)).To(BeTrue(), "the original error should be wrapped")
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
			g.Expect(available).To(Equal(tt.wantAvailable))
			g.Expect(*calls).To(Equal(1))

			available, err = IsResourceAvailable(context.Background(), client, testGVR)
			g.Expect(err).NotTo(HaveOccurred())
			if tt.wantCached {
				g.Expect(*calls).To(Equal(1), "the result should have been served from the cache")
				g.Expect(available).To(Equal(tt.wantAvailable))
			} else {
				g.Expect(*calls).To(Equal(2), "the check should have been retried")
				g.Expect(available).To(BeTrue())
			}
		})
	}
}

func Test_IsResourceAvailable_UsesLimit(t *testing.T) {
	g := NewGomegaWithT(t)
	resetAvailableResourcesCache()
	client, _ := newFakeDynamicClient(nil)
	_, err := IsResourceAvailable(context.Background(), client, testGVR)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(client.Actions()).To(HaveLen(1))
	listAction, ok := client.Actions()[0].(k8stesting.ListActionImpl)
	g.Expect(ok).To(BeTrue())
	g.Expect(listAction.ListOptions.Limit).To(Equal(int64(1)))
}

// ctxAwareClient is a dynamic client whose List calls are handled by the list function, which receives the request
// context. Only Resource(...).Namespace(...).List(...) is implemented: any other call panics.
type ctxAwareClient struct {
	dynamic.Interface
	list func(ctx context.Context) error
}

func (c *ctxAwareClient) Resource(schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &ctxAwareResource{list: c.list}
}

type ctxAwareResource struct {
	dynamic.NamespaceableResourceInterface
	list func(ctx context.Context) error
}

func (r *ctxAwareResource) Namespace(string) dynamic.ResourceInterface {
	return r
}

func (r *ctxAwareResource) List(ctx context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	if err := r.list(ctx); err != nil {
		return nil, err
	}
	return &unstructured.UnstructuredList{}, nil
}

// isResourceAvailableWithin calls IsResourceAvailable and fails the test if it does not return within d, so that a
// missing timeout or cancellation handling makes the test fail fast instead of hanging.
func isResourceAvailableWithin(t *testing.T, d time.Duration, ctx context.Context, client dynamic.Interface) (bool, error) {
	t.Helper()
	type result struct {
		available bool
		err       error
	}
	done := make(chan result, 1)
	go func() {
		available, err := IsResourceAvailable(ctx, client, testGVR)
		done <- result{available, err}
	}()
	select {
	case r := <-done:
		return r.available, r.err
	case <-time.After(d):
		t.Fatalf("IsResourceAvailable did not return within %s", d)
		return false, nil
	}
}

func Test_IsResourceAvailable_Timeout(t *testing.T) {
	t.Run("the request has the configured deadline", func(t *testing.T) {
		g := NewGomegaWithT(t)
		resetAvailableResourcesCache()
		var deadline time.Time
		var hasDeadline bool
		client := &ctxAwareClient{list: func(ctx context.Context) error {
			deadline, hasDeadline = ctx.Deadline()
			return nil
		}}

		start := time.Now()
		available, err := IsResourceAvailable(context.Background(), client, testGVR)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(available).To(BeTrue())
		g.Expect(hasDeadline).To(BeTrue(), "the request should have a deadline")
		g.Expect(deadline).To(BeTemporally("~", start.Add(resourceAvailabilityTimeout), time.Second))
	})

	t.Run("an earlier parent deadline is respected", func(t *testing.T) {
		g := NewGomegaWithT(t)
		resetAvailableResourcesCache()
		var deadline time.Time
		client := &ctxAwareClient{list: func(ctx context.Context) error {
			deadline, _ = ctx.Deadline()
			return nil
		}}
		parent, cancel := context.WithTimeout(context.Background(), resourceAvailabilityTimeout/10)
		defer cancel()
		parentDeadline, _ := parent.Deadline()

		_, err := IsResourceAvailable(parent, client, testGVR)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(deadline).To(Equal(parentDeadline))
	})

	t.Run("a hanging request returns on timeout and is not cached", func(t *testing.T) {
		g := NewGomegaWithT(t)
		resetAvailableResourcesCache()
		defaultTimeout := resourceAvailabilityTimeout
		resourceAvailabilityTimeout = 50 * time.Millisecond
		t.Cleanup(func() { resourceAvailabilityTimeout = defaultTimeout })
		calls := 0
		client := &ctxAwareClient{list: func(ctx context.Context) error {
			calls++
			if calls == 1 {
				// blocks until done on call 1
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		}}

		available, err := isResourceAvailableWithin(t, 5*time.Second, context.Background(), client)
		g.Expect(err).To(MatchError(context.DeadlineExceeded))
		g.Expect(available).To(BeFalse())

		available, err = IsResourceAvailable(context.Background(), client, testGVR)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(available).To(BeTrue())
		g.Expect(calls).To(Equal(2), "the timed out check should not have been cached")
	})

	t.Run("a hanging request returns on parent cancellation and is not cached", func(t *testing.T) {
		g := NewGomegaWithT(t)
		resetAvailableResourcesCache()
		calls := 0
		client := &ctxAwareClient{list: func(ctx context.Context) error {
			calls++
			if calls == 1 {
				// blocks until done on call 1
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		}}
		parent, cancel := context.WithCancel(context.Background())
		time.AfterFunc(50*time.Millisecond, cancel)

		available, err := isResourceAvailableWithin(t, 5*time.Second, parent, client)
		g.Expect(err).To(MatchError(context.Canceled))
		g.Expect(available).To(BeFalse())

		available, err = IsResourceAvailable(context.Background(), client, testGVR)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(available).To(BeTrue())
		g.Expect(calls).To(Equal(2), "the cancelled check should not have been cached")
	})
}
