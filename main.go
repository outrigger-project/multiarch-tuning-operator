/*
Copyright 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1beta1"
	//+kubebuilder:scaffold:imports

	ocpv1 "github.com/openshift/api/config/v1"
	ocpv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"github.com/openshift/library-go/pkg/operator/events"

	multiarchv1alpha1 "github.com/openshift/multiarch-tuning-operator/apis/multiarch/v1alpha1"
	"github.com/openshift/multiarch-tuning-operator/controllers/operator"
	"github.com/openshift/multiarch-tuning-operator/controllers/podplacement"
	"github.com/openshift/multiarch-tuning-operator/pkg/utils"
)

const (
	unableToCreateController = "unable to create controller"
	unableToAddRunnable      = "unable to add runnable"
	controllerKey            = "controller"
	runnableKey              = "runnable"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
	metricsAddr,
	probeAddr,
	certDir,
	globalPullSecretNamespace,
	globalPullSecretName,
	registryCertificatesConfigMapNamespace,
	registryCertificatesConfigMapName string
	enableLeaderElection,
	enableClusterPodPlacementConfigOperandWebHook,
	enableClusterPodPlacementConfigOperandControllers,
	enableOperator bool
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(multiarchv1alpha1.AddToScheme(scheme))

	// TODO[OCP specific]
	utilruntime.Must(ocpv1.Install(scheme))
	utilruntime.Must(ocpv1alpha1.Install(scheme))

	utilruntime.Must(multiarchv1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	bindFlags()
	must(validateFlags(), "invalid flags")
	// Build the leader election ID deterministically and based on the flags
	leaderId := "208d7abd.multiarch.openshift.io"
	if enableOperator {
		leaderId = fmt.Sprintf("operator-%s", leaderId)
	}
	if enableClusterPodPlacementConfigOperandControllers {
		leaderId = fmt.Sprintf("ppc-controllers-%s", leaderId)
	}
	webhookServer := webhook.NewServer(webhook.Options{
		Port:    9443,
		CertDir: certDir,
	})
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
			CertDir:     certDir,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       leaderId,
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	must(err, "unable to create manager")

	//+kubebuilder:scaffold:builder
	must(mgr.AddHealthzCheck("healthz", healthz.Ping), "unable to set up health check")
	must(mgr.AddReadyzCheck("readyz", healthz.Ping), "unable to set up ready check")

	if enableOperator {
		RunOperator(mgr)
	}
	if enableClusterPodPlacementConfigOperandControllers {
		RunClusterPodPlacementConfigOperandControllers(mgr)
	}
	if enableClusterPodPlacementConfigOperandWebHook {
		RunClusterPodPlacementConfigOperandWebHook(mgr)
	}

	setupLog.Info("starting manager")
	must(mgr.Start(ctrl.SetupSignalHandler()), "unable to start the manager")
}

func RunOperator(mgr ctrl.Manager) {
	config := ctrl.GetConfigOrDie()
	clientset := kubernetes.NewForConfigOrDie(config)
	gvk, _ := apiutil.GVKForObject(&multiarchv1alpha1.ClusterPodPlacementConfig{}, mgr.GetScheme())
	must((&operator.ClusterPodPlacementConfigReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		ClientSet: clientset,
		Recorder: events.NewKubeRecorder(clientset.CoreV1().Events(utils.Namespace()), utils.OperatorName, &corev1.ObjectReference{
			Kind:       gvk.Kind,
			Name:       multiarchv1alpha1.SingletonResourceObjectName,
			Namespace:  utils.Namespace(),
			APIVersion: gvk.GroupVersion().String(),
		}),
	}).SetupWithManager(mgr), unableToCreateController, controllerKey, "ClusterPodPlacementConfig")
	must((&multiarchv1alpha1.ClusterPodPlacementConfig{}).SetupWebhookWithManager(mgr), unableToCreateController,
		controllerKey, "ClusterPodPlacementConfigConversionWebhook")
}

func RunClusterPodPlacementConfigOperandControllers(mgr ctrl.Manager) {
	config := ctrl.GetConfigOrDie()
	clientset := kubernetes.NewForConfigOrDie(config)

	must((&podplacement.PodReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		ClientSet: clientset,
	}).SetupWithManager(mgr),
		unableToCreateController, controllerKey, "PodReconciler")

	must(mgr.Add(podplacement.NewConfigSyncerRunnable()), unableToAddRunnable, runnableKey, "ConfigSyncerRunnable")
	must(mgr.Add(podplacement.NewRegistryCertificatesSyncer(clientset, registryCertificatesConfigMapNamespace,
		registryCertificatesConfigMapName)),
		unableToAddRunnable, runnableKey, "RegistryCertificatesSyncer")
	must(mgr.Add(podplacement.NewGlobalPullSecretSyncer(clientset, globalPullSecretNamespace, globalPullSecretName)),
		unableToAddRunnable, runnableKey, "GlobalPullSecretSyncer")

	// TODO[OCP specific]
	must(mgr.Add(podplacement.NewICSPSyncer(mgr)),
		unableToAddRunnable, runnableKey, "ICSPSyncer")
	must(mgr.Add(podplacement.NewIDMSSyncer(mgr)),
		unableToAddRunnable, runnableKey, "IDMSSyncer")
	must(mgr.Add(podplacement.NewITMSSyncer(mgr)),
		unableToAddRunnable, runnableKey, "ITMSSyncer")
	must(mgr.Add(podplacement.NewImageRegistryConfigSyncer(mgr)),
		unableToAddRunnable, runnableKey, "ImageRegistryConfigSyncer")
}

func RunClusterPodPlacementConfigOperandWebHook(mgr ctrl.Manager) {
	mgr.GetWebhookServer().Register("/add-pod-scheduling-gate", &webhook.Admission{Handler: &podplacement.PodSchedulingGateMutatingWebHook{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}})
}

func validateFlags() error {
	if !enableOperator && !enableClusterPodPlacementConfigOperandControllers && !enableClusterPodPlacementConfigOperandWebHook {
		return errors.New("at least one of the following flags must be set: --enable-operator, --enable-ppc-controllers, --enable-ppc-webhook")
	}
	return nil
}

func bindFlags() {
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&certDir, "cert-dir", "/var/run/manager/tls", "The directory where the TLS certs are stored")
	// TODO: Change the defaults to match a local secret; the OCP specific settings will be provided by the operator
	flag.StringVar(&globalPullSecretNamespace, "global-pull-secret-namespace", "openshift-config", "The namespace where the global pull secret is stored")
	flag.StringVar(&globalPullSecretName, "global-pull-secret-name", "pull-secret", "The name of the global pull secret")
	flag.StringVar(&registryCertificatesConfigMapNamespace, "registry-certificates-configmap-namespace", "openshift-image-registry", "The namespace where the configmap that contains the registry certificates is stored")
	flag.StringVar(&registryCertificatesConfigMapName, "registry-certificates-configmap-name", "image-registry-certificates", "The name of the configmap that contains the registry certificates")
	flag.BoolVar(&enableClusterPodPlacementConfigOperandWebHook, "enable-ppc-webhook", false, "Enable the pod placement config operand webhook")
	flag.BoolVar(&enableClusterPodPlacementConfigOperandControllers, "enable-ppc-controllers", false, "Enable the pod placement config operand controllers")
	flag.BoolVar(&enableOperator, "enable-operator", false, "Enable the operator")
	opts := zap.Options{
		Development: true,
	}
	klog.InitFlags(nil)
	_ = flag.Set("alsologtostderr", "true")

	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
}

func must(err error, msg string, keysAndValues ...interface{}) {
	if err != nil {
		setupLog.Error(err, msg, keysAndValues...)
		os.Exit(1)
	}
}
