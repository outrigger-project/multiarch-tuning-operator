package metrics

import (
	"sync"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"

	metrics2 "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	ProcessedPodsWH prometheus.Counter
	GatedPods       prometheus.Counter
	ResponseTime    prometheus.Histogram
)

var onceWebhook sync.Once

func InitWebhookMetrics() {
	onceWebhook.Do(initWebhookMetrics)
}

func initWebhookMetrics() {
	initCommonMetrics()
	ProcessedPodsWH = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mto_ppo_wh_pods_processed_total",
			Help: "The total number of pods processed by the webhook",
		},
	)
	GatedPods = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mto_ppo_wh_pods_gated_total",
			Help: "The total number of pods gated by the webhook",
		},
	)

	ResponseTime = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "mto_ppo_wh_response_time_seconds",
			Help:    "The response time of the webhook",
			Buckets: utils.Buckets(),
		},
	)
	metrics2.Registry.MustRegister(ProcessedPodsWH, GatedPods, ResponseTime)
}
