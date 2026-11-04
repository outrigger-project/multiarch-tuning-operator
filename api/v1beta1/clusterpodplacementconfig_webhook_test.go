package v1beta1

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

func TestClusterPodPlacementConfigDefaultProtectsCreation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		finalizers []string
	}{
		{name: "new object"},
		{name: "preserves another finalizer", finalizers: []string{"example.com/cleanup"}},
		{name: "already protected", finalizers: []string{utils.PodPlacementFinalizerName}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cppc := &ClusterPodPlacementConfig{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Finalizers: append([]string(nil), tc.finalizers...)}}
			defaulter := &ClusterPodPlacementConfigDefaulter{}
			for i := 0; i < 2; i++ {
				if err := defaulter.Default(t.Context(), cppc); err != nil {
					t.Fatal(err)
				}
			}
			count := 0
			for _, finalizer := range cppc.Finalizers {
				if finalizer == utils.PodPlacementFinalizerName {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("expected exactly one cleanup finalizer, got %v", cppc.Finalizers)
			}
			if tc.name == "preserves another finalizer" && (len(cppc.Finalizers) != 2 || cppc.Finalizers[0] != "example.com/cleanup") {
				t.Fatalf("other finalizers were changed: %v", cppc.Finalizers)
			}
		})
	}
}

type deleteValidationClient struct {
	client.Client
	listErr error
}

func (c deleteValidationClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if c.listErr != nil {
		return c.listErr
	}
	return c.Client.List(ctx, list, opts...)
}

func TestClusterPodPlacementConfigValidatorValidateDelete(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	ppc := &PodPlacementConfig{ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: "another-namespace"}}
	cacheErr := errors.New("cache list failed")
	apiErr := errors.New("API list failed")
	for _, tc := range []struct {
		name          string
		cached, live  []client.Object
		cacheErr      error
		apiErr        error
		wantErr       error
		wantRemaining bool
	}{
		{name: "stale empty cache cannot allow deletion", live: []client.Object{ppc}, wantRemaining: true},
		{name: "cached PPC blocks deletion without an API read", cached: []client.Object{ppc}, apiErr: apiErr, wantRemaining: true},
		{name: "deletion allowed when cache and API are empty"},
		{name: "cache errors block deletion", cacheErr: cacheErr, wantErr: cacheErr},
		{name: "API fallback errors block deletion", apiErr: apiErr, wantErr: apiErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cached := deleteValidationClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.cached...).Build(), listErr: tc.cacheErr}
			live := deleteValidationClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.live...).Build(), listErr: tc.apiErr}
			validator := &ClusterPodPlacementConfigValidator{Client: cached, APIReader: live}
			warnings, err := validator.ValidateDelete(t.Context(), &ClusterPodPlacementConfig{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}})
			if len(warnings) != 0 {
				t.Fatalf("unexpected warnings: %v", warnings)
			}
			switch {
			case tc.wantRemaining:
				if err == nil || err.Error() != "cannot delete ClusterPodPlacementConfig while local PodPlacementConfigs still exist" {
					t.Fatalf("expected remaining-PPC rejection, got %v", err)
				}
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected wrapped %v, got %v", tc.wantErr, err)
				}
			case err != nil:
				t.Fatalf("unexpected rejection: %v", err)
			}
		})
	}
}
