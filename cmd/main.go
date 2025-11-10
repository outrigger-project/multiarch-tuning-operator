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
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	//+kubebuilder:scaffold:imports

	"github.com/openshift/library-go/pkg/operator/events"

	"github.com/panjf2000/ants/v2"
	zapuber "go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	multiarchv1alpha1 "github.com/openshift/multiarch-tuning-operator/api/v1alpha1"
	multiarchv1beta1 "github.com/openshift/multiarch-tuning-operator/api/v1beta1"

	"github.com/openshift/multiarch-tuning-operator/api/common"
	enoexeceventhandler "github.com/openshift/multiarch-tuning-operator/internal/controller/enoexecevent/handler"
	"github.com/openshift/multiarch-tuning-operator/internal/controller/operator"
	"github.com/openshift/multiarch-tuning-operator/internal/controller/podplacement"
	"github.com/openshift/multiarch-tuning-operator/pkg/informers/clusterpodplacementconfig"
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
	registryCertificatesConfigMapName string
	enableLeaderElection,
	enableClusterPodPlacementConfigOperandWebHook,
	enableClusterPodPlacementConfigOperandControllers,
	enableENoExecEventControllers bool
	enableCPPCInformer bool
	enableOperator     bool
	initialLogLevel    int
	postFuncs          []func()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(multiarchv1alpha1.AddToScheme(scheme))
	utilruntime.Must(multiarchv1beta1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
}

func main() {
	bindFlags()
	must(validateFlags(), "invalid flags")
	cacheOpts := cache.Options{
		DefaultTransform: cache.TransformStripManagedFields(),
	}

	// Build the leader election ID deterministically and based on the flags
	leaderID := "208d7abd.multiarch.openshift.io"
	if enableOperator {
		leaderID = fmt.Sprintf("operator-%s", leaderID)
	}
	if enableClusterPodPlacementConfigOperandControllers {
		leaderID = fmt.Sprintf("ppc-controllers-%s", leaderID)
		// We need to watch the pods with the status.phase equal to Pending to be able to update the nodeAffinity.
		// We can discard the other pods because they are already scheduled.
		cacheOpts.ByObject = map[client.Object]cache.ByObject{
			&corev1.Pod{}: {
				Field: fields.OneTermEqualSelector("status.phase", "Pending"),
			},
		}
	}
	if enableENoExecEventControllers {
		leaderID = fmt.Sprintf("enoexecevent-controllers-%s", leaderID)
		cacheOpts.DefaultNamespaces = map[string]cache.Config{
			utils.Namespace(): {},
		}
	}

	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	// - https://github.com/kubernetes-sigs/kubebuilder/blob/33a2f3dc556a9e49e06e6f19e0ae737d82d402db/testdata/project-v4/cmd/main.go#L78-L89
	var tlsOpts []func(*tls.Config)
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}
	tlsOpts = append(tlsOpts, disableHTTP2)

	webhookServer := webhook.NewServer(webhook.Options{
		Port:    9443,
		CertDir: certDir,
		TLSOpts: tlsOpts,
	})
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:    metricsAddr,
			CertDir:        certDir,
			FilterProvider: filters.WithAuthenticationAndAuthorization,
			SecureServing:  true,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       leaderID,
		Cache:                  cacheOpts,
		Logger:                 ctrllog.FromContext(context.Background()).WithName("manager"),
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

	if enableCPPCInformer {
		must(mgr.Add(clusterpodplacementconfig.NewCPPCSyncer(mgr)), "unable to instantiate CPPCSyncer")
	}

	if enableOperator {
		RunOperator(mgr)
	}
	if enableClusterPodPlacementConfigOperandControllers {
		RunClusterPodPlacementConfigOperandControllers(mgr)
	}
	if enableClusterPodPlacementConfigOperandWebHook {
		RunClusterPodPlacementConfigOperandWebHook(mgr)
	}
	if enableENoExecEventControllers {
		RunENoExecEventControllers(mgr)
	}

	setupLog.Info("starting manager")
	must(mgr.Start(ctrl.SetupSignalHandler()), "unable to start the manager")
	setupLog.Info("the manager has stopped")
	setupLog.Info("running post functions")
	for _, f := range postFuncs {
		f()
	}
	setupLog.Info("exiting")
}

func RunOperator(mgr ctrl.Manager) {
	config := ctrl.GetConfigOrDie()
	clientset := kubernetes.NewForConfigOrDie(config)

	// Get GVK for ClusterPodPlacementConfig
	gvk, _ := apiutil.GVKForObject(&multiarchv1beta1.ClusterPodPlacementConfig{}, mgr.GetScheme())

	// Set up the reconciler
	must((&operator.ClusterPodPlacementConfigReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		ClientSet:     clientset,
		DynamicClient: dynamic.NewForConfigOrDie(config),
		Recorder: events.NewKubeRecorder(
			clientset.CoreV1().Events(utils.Namespace()),
			utils.OperatorName,
			&corev1.ObjectReference{
				Kind:       gvk.Kind,
				Name:       common.SingletonResourceObjectName,
				Namespace:  utils.Namespace(),
				APIVersion: gvk.GroupVersion().String(),
			},
			clock.RealClock{},
		),
	}).SetupWithManager(mgr), unableToCreateController, controllerKey, "ClusterPodPlacementConfig")
	must((&multiarchv1beta1.ClusterPodPlacementConfig{}).SetupWebhookWithManager(mgr), unableToCreateController,
		controllerKey, "ClusterPodPlacementConfigConversionWebhook")
}

func RunClusterPodPlacementConfigOperandControllers(mgr ctrl.Manager) {
	config := ctrl.GetConfigOrDie()
	clientset := kubernetes.NewForConfigOrDie(config)

	must((&podplacement.PodReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		ClientSet: clientset,
		Recorder:  mgr.GetEventRecorderFor(utils.OperatorName),
	}).SetupWithManager(mgr),
		unableToCreateController, controllerKey, "PodReconciler")

	must(mgr.Add(podplacement.NewGlobalPullSecretSyncer(clientset, globalPullSecretNamespace, globalPullSecretName)),
		unableToAddRunnable, runnableKey, "GlobalPullSecretSyncer")
}

func RunClusterPodPlacementConfigOperandWebHook(mgr ctrl.Manager) {
	config := ctrl.GetConfigOrDie()
	clientset := kubernetes.NewForConfigOrDie(config)
	pool, err := ants.NewMultiPool(16, 16, ants.LeastTasks,
		ants.WithPreAlloc(true))
	must(err, "unable to create multi pool for the webhook's event messages")
	postFuncs = append(postFuncs, func() {
		err = pool.ReleaseTimeout(30 * time.Second)
		if err != nil {
			setupLog.Error(err, "failed to release the worker pool")
		}
		ants.Release()
	})
	handler := podplacement.NewPodSchedulingGateMutatingWebHook(mgr.GetClient(), clientset, mgr.GetScheme(),
		mgr.GetEventRecorderFor(utils.OperatorName), pool)
	mgr.GetWebhookServer().Register("/add-pod-scheduling-gate", &webhook.Admission{Handler: handler})
}

func RunENoExecEventControllers(mgr ctrl.Manager) {
	config := ctrl.GetConfigOrDie()
	clientset := kubernetes.NewForConfigOrDie(config)
	must(enoexeceventhandler.NewReconciler(
		mgr.GetClient(),
		clientset,
		mgr.GetScheme(),
		mgr.GetEventRecorderFor(utils.EnoexecControllerName),
	).SetupWithManager(mgr), unableToCreateController, controllerKey, "ENoExecEventController")
}

func validateFlags() error {
	if !enableOperator && !enableClusterPodPlacementConfigOperandControllers && !enableClusterPodPlacementConfigOperandWebHook && !enableENoExecEventControllers {
		return errors.New("at least one of the following flags must be set: --enable-operator, --enable-ppc-controllers, --enable-ppc-webhook, --enable-enoexec-event-controllers")
	}
	// no more than one of the flags can be set
	if btoi(enableOperator)+btoi(enableClusterPodPlacementConfigOperandControllers)+btoi(enableClusterPodPlacementConfigOperandWebHook)+btoi(enableENoExecEventControllers) > 1 {
		return errors.New("only one of the following flags can be set: --enable-operator, --enable-ppc-controllers, --enable-ppc-webhook, --enable-enoexec-event-controllers")
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
	flag.StringVar(&registryCertificatesConfigMapName, "registry-certificates-configmap-name", "image-registry-certificates", "The name of the configmap that contains the registry certificates")
	flag.BoolVar(&enableClusterPodPlacementConfigOperandWebHook, "enable-ppc-webhook", false, "Enable the pod placement config operand webhook")
	flag.BoolVar(&enableClusterPodPlacementConfigOperandControllers, "enable-ppc-controllers", false, "Enable the pod placement config operand controllers")
	flag.BoolVar(&enableOperator, "enable-operator", false, "Enable the operator")
	flag.BoolVar(&enableCPPCInformer, "enable-cppc-informer", false, "Enable informer for ClusterPodPlacementConfig")
	flag.BoolVar(&enableENoExecEventControllers, "enable-enoexec-event-controllers", false, "Enable the ENoExecEvent controllers")
	// This may be deprecated in the future. It is used to support the current way of setting the log level for operands
	// If operands will start to support a controller that watches the ClusterPodPlacementConfig, this flag may be removed
	// and the log level will be set in the ClusterPodPlacementConfig at runtime (with no need for reconciliation)
	flag.IntVar(&initialLogLevel, "initial-log-level", common.LogVerbosityLevelNormal.ToZapLevelInt(), "Initial log level. Converted to zap")
	klog.InitFlags(nil)
	flag.Parse()
	// Set the Log Level as AtomicLevel to allow runtime changes
	utils.AtomicLevel = zapuber.NewAtomicLevelAt(zapcore.Level(int8(-initialLogLevel))) // #nosec G115 -- initialLogLevel is constrained to 0-3 range
	zapLogger := zap.New(zap.Level(utils.AtomicLevel), zap.UseDevMode(false))
	klog.SetLogger(zapLogger)
	ctrllog.SetLogger(zapLogger)
}

func must(err error, msg string, keysAndValues ...interface{}) {
	if err != nil {
		setupLog.Error(err, msg, keysAndValues...)
		os.Exit(1)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
