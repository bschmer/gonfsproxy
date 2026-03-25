package metrics

import "github.com/prometheus/client_golang/prometheus"

type Set struct {
	RPCDelayAppliedTotal       prometheus.CounterVec
	RPCDelaySecondsTotal       prometheus.CounterVec
	RPCDelayInjectedSeconds    prometheus.HistogramVec
	RPCDropAppliedTotal        prometheus.CounterVec
	CallForwardedTotal         prometheus.CounterVec
	RPCBackendRoundTripSeconds prometheus.HistogramVec
	RPCRoundTripSeconds        prometheus.HistogramVec
}

func New(reg prometheus.Registerer) *Set {
	m := &Set{
		RPCDelayAppliedTotal: *prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rpc_injection_delay_applied_total",
			Help: "Count of injected RPC reply delays",
		}, []string{"procedure"}),
		RPCDelaySecondsTotal: *prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rpc_injection_delay_seconds_total",
			Help: "Total injected RPC delay seconds",
		}, []string{"procedure", "mode"}),
		RPCDelayInjectedSeconds: *prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "rpc_injection_delay_seconds",
			Help:    "Distribution of injected RPC delay seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		}, []string{"procedure", "mode"}),
		RPCDropAppliedTotal: *prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rpc_injection_drop_applied_total",
			Help: "Count of injected RPC connection drops",
		}, []string{"procedure"}),
		CallForwardedTotal: *prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rpc_call_forwarded_total",
			Help: "Count of forwarded RPC calls for NFS program",
		}, []string{"procedure"}),
		RPCBackendRoundTripSeconds: *prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "rpc_backend_roundtrip_seconds",
			Help:    "Observed backend RPC round-trip time in proxy (forward call to backend reply)",
			Buckets: []float64{0.0001, 0.00025, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		}, []string{"program", "procedure"}),
		RPCRoundTripSeconds: *prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "rpc_roundtrip_seconds",
			Help:    "Observed client-visible end-to-end RPC round-trip time in proxy (forward call to client reply, includes injected delay)",
			Buckets: []float64{0.0001, 0.00025, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		}, []string{"program", "procedure"}),
	}

	reg.MustRegister(&m.RPCDelayAppliedTotal)
	reg.MustRegister(&m.RPCDelaySecondsTotal)
	reg.MustRegister(&m.RPCDelayInjectedSeconds)
	reg.MustRegister(&m.RPCDropAppliedTotal)
	reg.MustRegister(&m.CallForwardedTotal)
	reg.MustRegister(&m.RPCBackendRoundTripSeconds)
	reg.MustRegister(&m.RPCRoundTripSeconds)
	return m
}
