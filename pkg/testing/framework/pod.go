package framework

import (
	"bytes"
	"context"
	"fmt"
	"log"

	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
)

func GetPodsWithLabel(ctx context.Context, client runtimeclient.Client, namespace, labelKey, labelInValue string) (*v1.PodList, error) {
	r, err := labels.NewRequirement(labelKey, "in", []string{labelInValue})
	labelSelector := labels.NewSelector().Add(*r)
	Expect(err).NotTo(HaveOccurred())
	pods := &v1.PodList{}
	err = client.List(ctx, pods, &runtimeclient.ListOptions{
		Namespace:     namespace,
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods found with label %s=%s in namespace %s", labelKey, labelInValue, namespace)
	}
	return pods, nil
}

func GetPodLog(ctx context.Context, clientset *kubernetes.Clientset, namespace, podName, containerName string) (string, error) {
	podLogOpts := v1.PodLogOptions{
		Container: containerName,
	}
	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, &podLogOpts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get logs for pod %s: %w", podName, err)
	}
	defer func() {
		if err := podLogs.Close(); err != nil {
			log.Printf("Error closing logs for pod %s: %v", podName, err)
		}
	}()

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(podLogs)
	if err != nil {
		return "", fmt.Errorf("failed to read logs for pod %s: %w", podName, err)
	}

	return buf.String(), nil
}

func StorePodsLog(ctx context.Context, clientset *kubernetes.Clientset, client runtimeclient.Client, namespace, labelKey, labelInValue, containerName, dir string) error {
	pods, err := GetPodsWithLabel(ctx, client, namespace, labelKey, labelInValue)
	if err != nil {
		return err
	}

	var errs []error
	for _, pod := range pods.Items {
		log.Printf("Getting logs for pod %s", pod.Name)
		logs, err := GetPodLog(ctx, clientset, namespace, pod.Name, containerName)
		if err != nil {
			log.Printf("Failed to get logs for pod %s: %v", pod.Name, err)
			errs = append(errs, fmt.Errorf("get logs for pod %s: %w", pod.Name, err))
			continue
		}
		err = WriteToFile(dir, fmt.Sprintf("%s.log", pod.Name), logs)
		if err != nil {
			log.Printf("Failed to write logs to file for pod %s: %v", pod.Name, err)
			errs = append(errs, fmt.Errorf("write logs for pod %s: %w", pod.Name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors occurred while storing pod logs: %v", errs)
	}
	return nil
}

func VerifyPodsAreRunning(ctx context.Context, client runtimeclient.Client, ns *v1.Namespace, labelKey string, labelInValue string) func(Gomega) {
	return func(g Gomega) {
		r, err := labels.NewRequirement(labelKey, selection.In, []string{labelInValue})
		labelSelector := labels.NewSelector().Add(*r)
		g.Expect(err).NotTo(HaveOccurred())
		pods := &v1.PodList{}
		err = client.List(ctx, pods, &runtimeclient.ListOptions{
			Namespace:     ns.Name,
			LabelSelector: labelSelector,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(pods.Items).NotTo(BeEmpty())
		g.Expect(pods.Items).Should(HaveEach(WithTransform(func(p v1.Pod) v1.PodPhase {
			return p.Status.Phase
		}, Equal(v1.PodRunning))))
	}
}

func VerifyPodNodeAffinity(ctx context.Context, client runtimeclient.Client, ns *v1.Namespace, labelKey string, labelInValue string, nodeSelectorTerms ...v1.NodeSelectorTerm) func(Gomega) {
	return func(g Gomega) {
		r, err := labels.NewRequirement(labelKey, "in", []string{labelInValue})
		labelSelector := labels.NewSelector().Add(*r)
		g.Expect(err).NotTo(HaveOccurred())
		pods := &v1.PodList{}
		err = client.List(ctx, pods, &runtimeclient.ListOptions{
			Namespace:     ns.Name,
			LabelSelector: labelSelector,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(pods.Items).NotTo(BeEmpty())
		if len(nodeSelectorTerms) == 0 {
			g.Expect(pods.Items).To(HaveEach(WithTransform(func(p v1.Pod) *v1.NodeSelector {
				if p.Spec.Affinity == nil || p.Spec.Affinity.NodeAffinity == nil {
					return nil
				}
				return p.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
			}, BeNil())))
		} else {
			g.Expect(pods.Items).To(HaveEach(HaveEquivalentNodeAffinity(
				&v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: nodeSelectorTerms,
					},
				})))
		}
	}
}

func VerifyPodPreferredNodeAffinity(ctx context.Context, client runtimeclient.Client, ns *v1.Namespace, labelKey string, labelInValue string, preferredSchedulingTerms []v1.PreferredSchedulingTerm) func(Gomega) {
	return func(g Gomega) {
		r, err := labels.NewRequirement(labelKey, "in", []string{labelInValue})
		g.Expect(err).NotTo(HaveOccurred())

		labelSelector := labels.NewSelector().Add(*r)
		pods := &v1.PodList{}
		err = client.List(ctx, pods, &runtimeclient.ListOptions{
			Namespace:     ns.Name,
			LabelSelector: labelSelector,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(pods.Items).NotTo(BeEmpty())

		if len(preferredSchedulingTerms) == 0 {
			g.Expect(pods.Items).To(HaveEach(WithTransform(func(p v1.Pod) []v1.WeightedPodAffinityTerm {
				if p.Spec.Affinity != nil && p.Spec.Affinity.PodAffinity != nil {
					return p.Spec.Affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution
				}
				return nil
			}, BeEmpty())))
		} else {
			g.Expect(pods.Items).To(HaveEach(HaveEquivalentPreferredNodeAffinity(
				&v1.NodeAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: preferredSchedulingTerms,
				})))
		}
	}
}

func VerifyDaemonSetPodNodeAffinity(ctx context.Context, client runtimeclient.Client, ns *v1.Namespace, labelKey string, labelInValue string, nodeSelectorRequirement *v1.NodeSelectorRequirement) func(g Gomega) {
	return func(g Gomega) {
		r, err := labels.NewRequirement(labelKey, "in", []string{labelInValue})
		labelSelector := labels.NewSelector().Add(*r)
		g.Expect(err).NotTo(HaveOccurred())
		pods := &v1.PodList{}
		err = client.List(ctx, pods, &runtimeclient.ListOptions{
			Namespace:     ns.Name,
			LabelSelector: labelSelector,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(pods.Items).NotTo(BeEmpty())
		for i := 0; i < len(pods.Items); i++ {
			pod := pods.Items[i]
			nodename := pod.Spec.NodeName
			nodenameNSR := builder.NewNodeSelectorRequirement().
				WithKeyAndValues("metadata.name", v1.NodeSelectorOpIn, nodename).
				Build()
			var expectedNSTs *v1.NodeSelectorTerm
			if nodeSelectorRequirement == nil {
				expectedNSTs = builder.NewNodeSelectorTerm().WithMatchFields(nodenameNSR).Build()
			} else {
				expectedNSTs = builder.NewNodeSelectorTerm().WithMatchExpressions(nodeSelectorRequirement).WithMatchFields(nodenameNSR).Build()
			}

			g.Expect([]v1.Pod{pod}).To(HaveEach(HaveEquivalentNodeAffinity(
				&v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{*expectedNSTs},
					},
				})))
		}
	}
}

func VerifyDaemonSetPreferredPodNodeAffinity(ctx context.Context, client runtimeclient.Client, ns *v1.Namespace, labelKey string, labelInValue string, preferredSchedulingTerms []v1.PreferredSchedulingTerm) func(g Gomega) {
	return func(g Gomega) {
		r, err := labels.NewRequirement(labelKey, "in", []string{labelInValue})
		labelSelector := labels.NewSelector().Add(*r)
		g.Expect(err).NotTo(HaveOccurred())
		pods := &v1.PodList{}
		err = client.List(ctx, pods, &runtimeclient.ListOptions{
			Namespace:     ns.Name,
			LabelSelector: labelSelector,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(pods.Items).NotTo(BeEmpty())
		for i := 0; i < len(pods.Items); i++ {
			pod := pods.Items[i]
			if preferredSchedulingTerms == nil {
				g.Expect([]v1.Pod{pod}).To(HaveEach(HaveEquivalentPreferredNodeAffinity(
					&v1.NodeAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: nil,
					})))
			} else {
				g.Expect([]v1.Pod{pod}).To(HaveEach(HaveEquivalentPreferredNodeAffinity(
					&v1.NodeAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: preferredSchedulingTerms,
					})))
			}
		}
	}
}

func VerifyPodAnnotations(ctx context.Context, client runtimeclient.Client, ns *v1.Namespace, labelKey string, labelInValue string, entries map[string]string) func(g Gomega) {
	return func(g Gomega) {
		r, err := labels.NewRequirement(labelKey, "in", []string{labelInValue})
		labelSelector := labels.NewSelector().Add(*r)
		g.Expect(err).NotTo(HaveOccurred())
		pods := &v1.PodList{}
		err = client.List(ctx, pods, &runtimeclient.ListOptions{
			Namespace:     ns.Name,
			LabelSelector: labelSelector,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(pods.Items).NotTo(BeEmpty())
		for k, v := range entries {
			g.Expect(pods.Items).Should(HaveEach(WithTransform(func(p v1.Pod) map[string]string {
				return p.Annotations
			}, And(Not(BeEmpty()), HaveKeyWithValue(k, v)))))
		}
	}
}

func VerifyPodsEvents(ctx context.Context, client runtimeclient.Client, ns *v1.Namespace, labelKey, labelInValue, eventReason string) func(g Gomega) {
	return func(g Gomega) {
		// Build label selector
		r, err := labels.NewRequirement(labelKey, selection.In, []string{labelInValue})
		g.Expect(err).NotTo(HaveOccurred())

		pods := &v1.PodList{}
		err = client.List(ctx, pods, &runtimeclient.ListOptions{
			Namespace:     ns.Name,
			LabelSelector: labels.NewSelector().Add(*r),
		})
		g.Expect(err).NotTo(HaveOccurred())
		if len(pods.Items) == 0 {
			fmt.Printf("No pods found with label %s=%s, skipping event verification\n", labelKey, labelInValue)
			return
		}

		// List all events in the namespace
		events := &v1.EventList{}
		err = client.List(ctx, events, &runtimeclient.ListOptions{
			Namespace: ns.Name,
			FieldSelector: fields.AndSelectors(
				fields.OneTermEqualSelector("involvedObject.kind", "Pod"),
				fields.OneTermEqualSelector("reason", eventReason),
			),
		})
		g.Expect(err).NotTo(HaveOccurred())

		// Create a map for faster event lookup
		eventMap := make(map[types.UID]bool)
		for _, e := range events.Items {
			eventMap[e.InvolvedObject.UID] = true
		}
		// Verify each pod has at least one matching event
		g.Expect(pods.Items).Should(HaveEach(
			WithTransform(func(p v1.Pod) bool {
				return eventMap[p.UID]
			}, BeTrue()),
		), "not all pods in namespace %s received event with reason %q. "+
			"Found %d pods but only %d matching events",
			ns.Name, eventReason, len(pods.Items), len(events.Items))
	}
}

func VerifyPodLabels(ctx context.Context, client runtimeclient.Client, ns *v1.Namespace, labelKey string, labelInValue string, ifPresent bool, entries map[string]string) func(g Gomega) {
	return func(g Gomega) {
		r, err := labels.NewRequirement(labelKey, "in", []string{labelInValue})
		labelSelector := labels.NewSelector().Add(*r)
		g.Expect(err).NotTo(HaveOccurred())
		pods := &v1.PodList{}
		err = client.List(ctx, pods, &runtimeclient.ListOptions{
			Namespace:     ns.Name,
			LabelSelector: labelSelector,
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(pods.Items).NotTo(BeEmpty())
		for k, v := range entries {
			if ifPresent {
				g.Expect(pods.Items).Should(HaveEach(WithTransform(func(p v1.Pod) map[string]string {
					return p.Labels
				}, And(Not(BeEmpty()), HaveKeyWithValue(k, v)))))
			} else {
				g.Expect(pods.Items).Should(HaveEach(WithTransform(func(p v1.Pod) map[string]string {
					return p.Labels
				}, Not(HaveKey(k)))))
			}
		}
	}
}

func VerifyPodLabelsAreSet(ctx context.Context, client runtimeclient.Client, ns *v1.Namespace, labelKey string, labelInValue string, labelsKeyValuePair ...string) func(g Gomega) {
	return func(g Gomega) {
		if len(labelsKeyValuePair)%2 != 0 {
			// It's ok to panic as this is only used in unit tests.
			panic("the number of arguments must be even")
		}
		entries := make(map[string]string)
		for i := 0; i < len(labelsKeyValuePair); i += 2 {
			entries[labelsKeyValuePair[i]] = labelsKeyValuePair[i+1]
		}
		VerifyPodLabels(ctx, client, ns, labelKey, labelInValue, true, entries)(g)
	}
}
