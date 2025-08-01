package podplacementconfig

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/controllers/podplacement/metrics"
)

// +kubebuilder:webhook:path=/validate-multiarch-openshift-io-v1beta1-podplacementconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=multiarch.openshift.io,resources=podplacementconfigs,verbs=create;update;delete,versions=v1beta1,name=validate-podplacementconfig.multiarch.openshift.io,admissionReviewVersions=v1

type PodPlacementConfigWebhook struct {
	client    client.Client
	clientSet *kubernetes.Clientset
	decoder   admission.Decoder
	once      sync.Once
	scheme    *runtime.Scheme
}

func (w *PodPlacementConfigWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	w.once.Do(func() {
		w.decoder = admission.NewDecoder(w.scheme)
	})

	switch req.Operation {
	case admissionv1.Create, admissionv1.Update:
		var newPPC multiarchv1beta1.PodPlacementConfig
		if err := w.decoder.Decode(req, &newPPC); err != nil {
			return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to decode PodPlacementConfig: %w", err))
		}

		// Validate duplicate architectures in nodeAffinityScoring
		if newPPC.Spec.Plugins != nil && newPPC.Spec.Plugins.NodeAffinityScoring != nil {
			seen := make(map[string]struct{})
			for _, term := range newPPC.Spec.Plugins.NodeAffinityScoring.Platforms {
				if _, exists := seen[term.Architecture]; exists {
					return admission.Denied(fmt.Sprintf(
						"duplicate architecture %q found in .spec.plugins.nodeAffinityScoring.platforms", term.Architecture))
				}
				seen[term.Architecture] = struct{}{}
			}
		}

		// List all existing PodPlacementConfigs
		var existingPPCs multiarchv1beta1.PodPlacementConfigList
		if err := w.client.List(ctx, &existingPPCs, client.InNamespace(req.Namespace)); err != nil {
			return admission.Errored(http.StatusInternalServerError,
				fmt.Errorf("failed to list existing PodPlacementConfigs in namespace %q: %w", req.Namespace, err))
		}

		var oldPPC multiarchv1beta1.PodPlacementConfig
		var priorityChanged bool
		if req.Operation == admissionv1.Update {
			if err := w.decoder.DecodeRaw(req.OldObject, &oldPPC); err != nil {
				return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to decode old object: %w", err))
			}
			priorityChanged = oldPPC.Spec.Priority != newPPC.Spec.Priority
		}

		for _, existing := range existingPPCs.Items {
			if existing.Spec.Priority != newPPC.Spec.Priority {
				continue
			}
			if req.Operation == admissionv1.Update && existing.Name == req.Name && !priorityChanged {
				continue
			}
			return admission.Denied(fmt.Sprintf(
				"spec.priority %d already exists in namespace %q (used by %q)",
				newPPC.Spec.Priority, req.Namespace, existing.Name))
		}

		return admission.Allowed("valid PodPlacementConfig")

	case admissionv1.Delete:
		var oldPPC multiarchv1beta1.PodPlacementConfig
		if err := w.decoder.DecodeRaw(req.OldObject, &oldPPC); err != nil {
			return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to decode old object on delete: %w", err))
		}

		// TODO(user): your logic here

		return admission.Allowed("delete allowed")

	default:
		return admission.Allowed("operation not handled explicitly")
	}
}

func NewPodPlacementConfigWebhook(client client.Client, clientSet *kubernetes.Clientset,
	scheme *runtime.Scheme) *PodPlacementConfigWebhook {
	a := &PodPlacementConfigWebhook{
		client:    client,
		clientSet: clientSet,
		scheme:    scheme,
	}
	metrics.InitWebhookMetrics()
	return a
}
