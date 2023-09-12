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
	"flag"
	commonsysconfig "multiarch-operator/controllers/sysconfig_handlers/common"
	openshiftsysconfig "multiarch-operator/controllers/sysconfig_handlers/openshift"
	"multiarch-operator/pkg/system_config"
	"os"
	"time"

	ocpv1 "github.com/openshift/api/config/v1"
	ocpv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/webhook"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	multiarchv1alpha1 "multiarch-operator/apis/multiarch/v1alpha1"
	podplacement "multiarch-operator/controllers/pod_placement"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const readonlySystemConfigResyncPeriod = 30 * time.Minute

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(multiarchv1alpha1.AddToScheme(scheme))

	// TODO[OCP specific]
	utilruntime.Must(ocpv1.Install(scheme))
	utilruntime.Must(ocpv1alpha1.Install(scheme))

	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var certDir string
	var globalPullSecretNamespace string
	var globalPullSecretName string
	var registryCertificatesConfigMapNamespace string
	var registryCertificatesConfigMapName string
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

	opts := zap.Options{
		Development: true,
	}
	klog.InitFlags(nil)
	flag.Set("alsologtostderr", "true")

	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "208d7abd.multiarch.openshift.io",
		CertDir:                certDir,
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
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	config := ctrl.GetConfigOrDie()
	clientset := kubernetes.NewForConfigOrDie(config)

	if err = (&podplacement.PodReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		ClientSet: clientset,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Pod")
		os.Exit(1)
	}
	if err = (&podplacement.PodPlacementConfigReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		ClientSet: clientset,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PodPlacementConfig")
		os.Exit(1)
	}

	err = mgr.Add(&system_config.ConfigSyncerRunnable{})
	if err != nil {
		setupLog.Error(err, "unable to add the ConfigSyncerRunnable to the manager")
		os.Exit(1)
	}

	err = mgr.Add(commonsysconfig.NewRegistryCertificatesSyncer(clientset, registryCertificatesConfigMapNamespace,
		registryCertificatesConfigMapName))
	if err != nil {
		setupLog.Error(err, "unable to add the ICSPSyncer Runnable to the manager")
		os.Exit(1)
	}

	err = mgr.Add(commonsysconfig.NewGlobalPullSecretSyncer(clientset, globalPullSecretNamespace, globalPullSecretName))
	if err != nil {
		setupLog.Error(err, "unable to add the ICSPSyncer Runnable to the manager")
		os.Exit(1)
	}

	// TODO[OCP specific]
	err = mgr.Add(openshiftsysconfig.NewICSPSyncer(mgr))
	if err != nil {
		setupLog.Error(err, "unable to add the ICSPSyncer Runnable to the manager")
		os.Exit(1)
	}

	err = mgr.Add(openshiftsysconfig.NewIDMSSyncer(mgr))
	if err != nil {
		setupLog.Error(err, "unable to add the IDMSSyncer Runnable to the manager")
		os.Exit(1)
	}

	err = mgr.Add(openshiftsysconfig.NewITMSSyncer(mgr))
	if err != nil {
		setupLog.Error(err, "unable to add the IDMSSyncer Runnable to the manager")
		os.Exit(1)
	}

	err = mgr.Add(openshiftsysconfig.NewImageRegistryConfigSyncer(mgr))
	if err != nil {
		setupLog.Error(err, "unable to add the ICSPSyncer Runnable to the manager")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	mgr.GetWebhookServer().Register("/add-pod-scheduling-gate", &webhook.Admission{Handler: &podplacement.PodSchedulingGateMutatingWebHook{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}})

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
