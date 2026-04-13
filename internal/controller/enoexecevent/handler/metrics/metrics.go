package metrics

import (
	"sync"

	metrics2 "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/prometheus/client_golang/prometheus"
)

var EnoexecCounter prometheus.Counter
var EnoexecCounterInvalid prometheus.Counter
var EnoexecCounterStale prometheus.Counter
var onceCommon sync.Once

func initMetrics() {
	onceCommon.Do(func() {
		EnoexecCounter = prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "mto_enoexecevents_total",
				Help: "The counter for exec format errors detected and reported",
			},
		)
		EnoexecCounterInvalid = prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "mto_enoexecevents_invalid_total",
				Help: "The counter for ENoExecEvents objects that failed reconciliation and were reported as pod events",
			},
		)
		EnoexecCounterStale = prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "mto_enoexecevents_stale_total",
				Help: "The counter for ENoExecEvents referencing pods/nodes that no longer exist",
			},
		)
		metrics2.Registry.MustRegister(EnoexecCounter)
		metrics2.Registry.MustRegister(EnoexecCounterInvalid)
		metrics2.Registry.MustRegister(EnoexecCounterStale)
	})
}

func InitMetrics() {
	initMetrics()
}
