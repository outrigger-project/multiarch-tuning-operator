package framework

import (
	"bytes"
	"context"
	"fmt"
	"log"

	. "github.com/onsi/gomega"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func VerifyPodsAreRunning(g Gomega, ctx context.Context, client runtimeclient.Client, ns *v1.Namespace, labelKey string, labelInValue string) {
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

func ExecInPodWithResult(ctx context.Context, clientset *kubernetes.Clientset, podRESTConfig *rest.Config, ns, podName, containerName string, command []string) (string, error) {
	u := clientset.CoreV1().RESTClient().Post().Resource("pods").Namespace(ns).Name(podName).SubResource("exec").VersionedParams(&v1.PodExecOptions{
		Container: containerName,
		Stdout:    true,
		Stderr:    true,
		Command:   command,
	}, scheme.ParameterCodec).URL()

	e, err := remotecommand.NewSPDYExecutor(podRESTConfig, "POST", u)
	if err != nil {
		return "", fmt.Errorf("could not initialize a new SPDY executor: %v", err)
	}
	buf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	if err := e.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: buf,
		Stdin:  nil,
		Stderr: errBuf,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

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

func GetPodLogs(ctx context.Context, clientset *kubernetes.Clientset, namespace, podName, containerName string) (string, error) {
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

func GetPodsLogToFile(ctx context.Context, clientset *kubernetes.Clientset, client runtimeclient.Client, namespace, labelKey, labelInValue, fileDir string) error {
	pods, err := GetPodsWithLabel(ctx, client, namespace, labelKey, labelInValue)
	if err != nil {
		return err
	}
	for _, pod := range pods.Items {
		log.Printf("Getting logs for pod %s", pod.Name)
		logs, err := GetPodLogs(ctx, clientset, utils.Namespace(), pod.Name, labelInValue)
		if err != nil {
			log.Printf("Failed to get logs for pod %s: %v", pod.Name, err)
			continue
		}
		err = WriteContentsToFile(fileDir, fmt.Sprintf("%s.log", pod.Name), logs)
		if err != nil {
			log.Printf("Failed to write logs to file: %v", err)
			return err
		} else {
			return nil
		}
	}
	return nil
}

func GetRegistriesConfFromPPCPodToFile(ctx context.Context, clientset *kubernetes.Clientset, client runtimeclient.Client, podRESTConfig *rest.Config, fileDir string) error {
	pods, err := GetPodsWithLabel(ctx, client, utils.Namespace(), "controller", utils.PodPlacementControllerName)
	if err != nil {
		return err
	}
	command := []string{"/bin/cat", "/etc/containers/registries.conf"}
	for _, pod := range pods.Items {
		log.Printf("Getting registries.conf for pod %s", pod.Name)
		contents, err := ExecInPodWithResult(ctx, clientset, podRESTConfig, utils.Namespace(), pod.Name, utils.PodPlacementControllerName, command)
		if err != nil {
			log.Printf("Failed to get registries.conf for pod %s: %v", pod.Name, err)
			continue
		}
		err = WriteContentsToFile(fileDir, fmt.Sprintf("%s-registries.conf", pod.Name), contents)
		if err != nil {
			log.Printf("Failed to write contents to file: %v", err)
			return err
		} else {
			return nil
		}
	}
	return nil
}
