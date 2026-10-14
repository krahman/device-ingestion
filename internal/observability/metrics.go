// Package observability provides low-cardinality Prometheus metrics for the
// ingestion service.
package observability

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Metrics struct {
	mu       sync.RWMutex
	counters map[string]uint64
	gauges   map[string]float64
	sums     map[string]float64
	counts   map[string]uint64
}

func NewMetrics() *Metrics {
	return &Metrics{
		counters: make(map[string]uint64),
		gauges:   make(map[string]float64),
		sums:     make(map[string]float64),
		counts:   make(map[string]uint64),
	}
}

func (m *Metrics) AddCounter(name string, labels map[string]string, amount uint64) {
	m.mu.Lock()
	m.counters[metricKey(name, labels)] += amount
	m.mu.Unlock()
}

func (m *Metrics) SetGauge(name string, value float64) {
	m.mu.Lock()
	m.gauges[name] = value
	m.mu.Unlock()
}

func (m *Metrics) Observe(name string, labels map[string]string, elapsed time.Duration) {
	key := metricKey(name, labels)
	m.mu.Lock()
	m.sums[key] += elapsed.Seconds()
	m.counts[key]++
	m.mu.Unlock()
}

// Handler renders metrics in Prometheus text exposition format. Database pool
// gauges are sampled at scrape time so they reflect the current sql.DB state.
func (m *Metrics) Handler(dbStats func() (open, inUse, idle int)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if dbStats != nil {
			open, inUse, idle := dbStats()
			m.SetGauge("device_ingestion_db_connections_open", float64(open))
			m.SetGauge("device_ingestion_db_connections_in_use", float64(inUse))
			m.SetGauge("device_ingestion_db_connections_idle", float64(idle))
		}
		m.mu.RLock()
		defer m.mu.RUnlock()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		keys := make([]string, 0, len(m.counters)+len(m.gauges)+len(m.sums)+len(m.counts))
		for key := range m.counters {
			keys = append(keys, key)
		}
		for key := range m.gauges {
			keys = append(keys, key)
		}
		for key := range m.sums {
			keys = append(keys, key+"#sum")
		}
		for key := range m.counts {
			keys = append(keys, key+"#count")
		}
		sort.Strings(keys)
		for _, key := range keys {
			if strings.HasSuffix(key, "#sum") {
				base := strings.TrimSuffix(key, "#sum")
				fmt.Fprintf(w, "%s %g\n", withSuffix(base, "_sum"), m.sums[base])
			} else if strings.HasSuffix(key, "#count") {
				base := strings.TrimSuffix(key, "#count")
				fmt.Fprintf(w, "%s %d\n", withSuffix(base, "_count"), m.counts[base])
			} else if value, ok := m.counters[key]; ok {
				fmt.Fprintf(w, "%s %d\n", key, value)
			} else {
				fmt.Fprintf(w, "%s %g\n", key, m.gauges[key])
			}
		}
	})
}

func withSuffix(key, suffix string) string {
	if i := strings.IndexByte(key, '{'); i >= 0 {
		return key[:i] + suffix + key[i:]
	}
	return key + suffix
}

func metricKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"").Replace(labels[key])
		parts = append(parts, key+`="`+value+`"`)
	}
	return name + "{" + strings.Join(parts, ",") + "}"
}
