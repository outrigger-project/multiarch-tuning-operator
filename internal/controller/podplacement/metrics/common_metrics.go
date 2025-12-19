package metrics

import (
	"sync"

	metrics2 "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/prometheus/client_golang/prometheus"
)

var GatedPodsGauge prometheus.Gauge
var onceCommon sync.Once

func initCommonMetrics() {
	onceCommon.Do(func() {
		GatedPodsGauge = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "mto_ppo_pods_gated",
				Help: "The current number of gated pods (this metric is not considered reliable yet)",
			},
		)
		metrics2.Registry.MustRegister(GatedPodsGauge)
	})
}
