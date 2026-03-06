package device

import (
	"sync"
	"time"
)

// =============================================================================
// Metrics Recorder Interface
// =============================================================================

// MetricsRecorder defines the interface for recording metrics.
// Implement this interface to integrate with external metrics systems
// (Prometheus, OpenTelemetry, etc.).
type MetricsRecorder interface {
	// IncCounter increments a counter metric.
	IncCounter(name string, value int64, labels map[string]string)

	// SetGauge sets a gauge metric.
	SetGauge(name string, value float64, labels map[string]string)

	// RecordHistogram records a histogram observation.
	RecordHistogram(name string, value float64, labels map[string]string)
}

// =============================================================================
// No-Op Metrics
// =============================================================================

// NoOpMetrics is a no-op implementation of MetricsRecorder.
// Use this as the default when no metrics system is configured.
type NoOpMetrics struct{}

// IncCounter is a no-op.
func (NoOpMetrics) IncCounter(string, int64, map[string]string) {}

// SetGauge is a no-op.
func (NoOpMetrics) SetGauge(string, float64, map[string]string) {}

// RecordHistogram is a no-op.
func (NoOpMetrics) RecordHistogram(string, float64, map[string]string) {}

// =============================================================================
// Global Metrics
// =============================================================================

var (
	metricsMu         sync.RWMutex
	globalMetrics     MetricsRecorder = NoOpMetrics{}
)

// SetMetricsRecorder sets the global metrics recorder.
// This should be called once at application startup.
func SetMetricsRecorder(m MetricsRecorder) {
	if m == nil {
		m = NoOpMetrics{}
	}
	metricsMu.Lock()
	defer metricsMu.Unlock()
	globalMetrics = m
}

// GetMetricsRecorder returns the current global metrics recorder.
func GetMetricsRecorder() MetricsRecorder {
	metricsMu.RLock()
	defer metricsMu.RUnlock()
	return globalMetrics
}

// =============================================================================
// Convenience Metric Functions
// =============================================================================

// IncCounter increments a counter metric using the global recorder.
func IncCounter(name string, value int64, labels map[string]string) {
	GetMetricsRecorder().IncCounter(name, value, labels)
}

// SetGauge sets a gauge metric using the global recorder.
func SetGauge(name string, value float64, labels map[string]string) {
	GetMetricsRecorder().SetGauge(name, value, labels)
}

// RecordHistogram records a histogram observation using the global recorder.
func RecordHistogram(name string, value float64, labels map[string]string) {
	GetMetricsRecorder().RecordHistogram(name, value, labels)
}

// =============================================================================
// WASM-Specific Metrics
// =============================================================================

// RecordWASMHeapUsage records the current WASM heap usage.
func RecordWASMHeapUsage(usedBytes, availableBytes uint64) {
	SetGauge("wasm_heap_used_bytes", float64(usedBytes), nil)
	SetGauge("wasm_heap_available_bytes", float64(availableBytes), nil)
}

// RecordBridgeLatency records a Go↔JS bridge call latency.
func RecordBridgeLatency(operation string, duration time.Duration) {
	RecordHistogram("wasm_bridge_latency_ms", float64(duration.Milliseconds()), map[string]string{
		"operation": operation,
	})
}

// RecordBridgeError records a bridge error and publishes a DeviceErrorEvent.
func RecordBridgeError(operation string, errorCode ErrorCode) {
	IncCounter("bridge_error_count", 1, map[string]string{
		"operation":  operation,
		"error_code": string(errorCode),
	})
	Publish(NewDeviceErrorEvent(operation, errorCode, "bridge error"))
}

// RecordInitDuration records the backend initialization duration.
func RecordInitDuration(backend string, duration time.Duration) {
	RecordHistogram("bridge_init_duration_ms", float64(duration.Milliseconds()), map[string]string{
		"backend": backend,
	})
}

// =============================================================================
// Timer Helper
// =============================================================================

// Timer is a helper for recording durations.
type Timer struct {
	name   string
	labels map[string]string
	start  time.Time
}

// StartTimer starts a new timer for the given name and labels.
func StartTimer(name string, labels map[string]string) *Timer {
	return &Timer{
		name:   name,
		labels: labels,
		start:  time.Now(),
	}
}

// Stop records the elapsed time and returns the duration.
func (t *Timer) Stop() time.Duration {
	duration := time.Since(t.start)
	RecordHistogram(t.name, float64(duration.Milliseconds()), t.labels)
	return duration
}

// StopWithLabels records the elapsed time with additional labels.
func (t *Timer) StopWithLabels(extraLabels map[string]string) time.Duration {
	labels := make(map[string]string)
	for k, v := range t.labels {
		labels[k] = v
	}
	for k, v := range extraLabels {
		labels[k] = v
	}
	duration := time.Since(t.start)
	RecordHistogram(t.name, float64(duration.Milliseconds()), labels)
	return duration
}
