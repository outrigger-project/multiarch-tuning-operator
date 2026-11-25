package operator

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	admissionv1 "k8s.io/api/admission/v1"
	apixv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	apiv1alpha1 "github.com/openshift/multiarch-tuning-operator/api/v1alpha1"
	apiv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"
	"github.com/openshift/multiarch-tuning-operator/pkg/testing/builder"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

// MULTIARCH-6528: regression coverage for the security hardening response
// headers on the admission and conversion webhook endpoints. The requests
// mirror the rapiDAST reproduction: nonpersisting AdmissionReview requests
// with dryRun=true and a v1alpha1-to-v1beta1 ConversionReview are sent
// directly to the webhook server, and the responses must carry both
// Strict-Transport-Security and X-Content-Type-Options: nosniff headers
// without breaking admission or conversion behavior.
var _ = Describe("Webhook security response headers", Ordered, Label("integration"), func() {
	var client *http.Client

	BeforeAll(func() {
		// The webhook server serves TLS with a certificate signed by the
		// envtest CA; trust it so the headers on real responses are verified.
		pool := x509.NewCertPool()
		Expect(pool.AppendCertsFromPEM(testEnv.WebhookInstallOptions.LocalServingCAData)).To(BeTrue())
		client = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool},
			},
		}
	})

	webhookURL := func(path string) string {
		return fmt.Sprintf("https://%s:%d%s",
			testEnv.WebhookInstallOptions.LocalServingHost,
			testEnv.WebhookInstallOptions.LocalServingPort, path)
	}

	postReview := func(path string, review interface{}) *http.Response {
		body, err := json.Marshal(review)
		Expect(err).NotTo(HaveOccurred())
		resp, err := client.Post(webhookURL(path), "application/json", bytes.NewReader(body))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	expectSecurityHeaders := func(resp *http.Response) {
		Expect(resp.Header.Get("Strict-Transport-Security")).To(Equal("max-age=31536000"))
		Expect(resp.Header.Get("X-Content-Type-Options")).To(Equal("nosniff"))
	}

	DescribeTable("webhook responses carry the security hardening headers",
		func(path string, review interface{}, verify func(*http.Response)) {
			resp := postReview(path, review)
			Expect(resp).To(HaveHTTPStatus(http.StatusOK))
			expectSecurityHeaders(resp)
			verify(resp)
		},
		Entry("valid ClusterPodPlacementConfig admission request is allowed",
			"/validate-multiarch-openshift-io-v1beta1-clusterpodplacementconfig",
			&admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "admission.k8s.io/v1",
					Kind:       "AdmissionReview",
				},
				Request: &admissionv1.AdmissionRequest{
					UID: "7e7b1b1e-7bd5-4d5e-8e6d-8ea1be4a4a4e",
					Kind: metav1.GroupVersionKind{
						Group:   "multiarch.openshift.io",
						Version: "v1beta1",
						Kind:    "ClusterPodPlacementConfig",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "multiarch.openshift.io",
						Version:  "v1beta1",
						Resource: "clusterpodplacementconfigs",
					},
					Operation: admissionv1.Create,
					DryRun:    utils.NewPtr(true),
					Object: runtime.RawExtension{
						Object: builder.NewClusterPodPlacementConfig().
							WithName("cluster").
							WithNamespaceSelector(&metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "test"},
							}).Build(),
					},
				},
			},
			func(resp *http.Response) {
				review := &admissionv1.AdmissionReview{}
				Expect(json.NewDecoder(resp.Body).Decode(review)).To(Succeed())
				Expect(review.Response).NotTo(BeNil())
				Expect(review.Response.Allowed).To(BeTrue(), review.Response.Result.Message)
			}),
		Entry("v1alpha1 to v1beta1 conversion request succeeds",
			"/convert",
			&apixv1.ConversionReview{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "admission.k8s.io/v1",
					Kind:       "ConversionReview",
				},
				Request: &apixv1.ConversionRequest{
					UID:               "9f2a92a2-70f8-484c-9743-bd1e1d7e5f32",
					DesiredAPIVersion: "multiarch.openshift.io/v1beta1",
					Objects: []runtime.RawExtension{
						{
							Object: &apiv1alpha1.ClusterPodPlacementConfig{
								TypeMeta: metav1.TypeMeta{
									APIVersion: "multiarch.openshift.io/v1alpha1",
									Kind:       "ClusterPodPlacementConfig",
								},
								ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
							},
						},
					},
				},
			},
			func(resp *http.Response) {
				review := &apixv1.ConversionReview{}
				Expect(json.NewDecoder(resp.Body).Decode(review)).To(Succeed())
				Expect(review.Response).NotTo(BeNil())
				Expect(review.Response.Result.Status).To(Equal(metav1.StatusSuccess))
				Expect(review.Response.ConvertedObjects).To(HaveLen(1))
				converted := &apiv1beta1.ClusterPodPlacementConfig{}
				Expect(json.Unmarshal(review.Response.ConvertedObjects[0].Raw, converted)).To(Succeed())
				Expect(converted.APIVersion).To(Equal("multiarch.openshift.io/v1beta1"))
				Expect(converted.Name).To(Equal("cluster"))
			}),
	)
})
