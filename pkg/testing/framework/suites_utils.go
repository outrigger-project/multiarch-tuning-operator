package framework

import (
	"context"

	"sigs.k8s.io/kustomize/api/resmap"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/gomega"
)

// ApplyCRDs applies the Custom Resource Definitions (CRDs) from the specified path to the test environment.
func ApplyCRDs(crdPath string, k8sClient client.Client, ctx context.Context) {
	klog.Infof("Applying CRDs to the test environment from path: %s", crdPath)
	kustomizer := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	resMap, err := kustomizer.Run(filesys.MakeFsOnDisk(), crdPath)
	Expect(err).NotTo(HaveOccurred())
	err = applyResources(resMap, k8sClient, ctx)
	Expect(err).NotTo(HaveOccurred())
}

func applyResources(resources resmap.ResMap, k8sClient client.Client, ctx context.Context) error {
	// Create a universal decoder for deserializing the resources
	decoder := scheme.Codecs.UniversalDeserializer()
	for _, res := range resources.Resources() {
		raw, err := res.AsYAML()
		Expect(err).NotTo(HaveOccurred())

		if len(raw) == 0 {
			return nil // Nothing to process
		}

		// Decode the resource from the buffer
		obj, _, err := decoder.Decode(raw, nil, nil)
		if err != nil {
			return err
		}

		// Check if the resource already exists
		existingObj := obj.DeepCopyObject().(client.Object)
		err = k8sClient.Get(context.TODO(), client.ObjectKey{
			Name:      existingObj.GetName(),
			Namespace: existingObj.GetNamespace(),
		}, existingObj)

		if err != nil && !errors.IsNotFound(err) {
			// Return error if it's not a "not found" error
			return err
		}
		if err == nil {
			// Resource exists, update it
			obj.(client.Object).SetResourceVersion(existingObj.GetResourceVersion())
			err = k8sClient.Update(ctx, obj.(client.Object))
			if err != nil {
				return err
			}
		} else {
			// Resource does not exist, create it
			err = k8sClient.Create(ctx, obj.(client.Object))
			if err != nil {
				return err
			}
		}
	}

	return nil
}
