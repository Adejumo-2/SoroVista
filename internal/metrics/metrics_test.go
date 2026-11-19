package metrics

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainWriter is a minimal http.ResponseWriter that deliberately does not
// implement http.Flusher, standing in for a transport without streaming
// support.
type plainWriter struct {
	header http.Header
	status int
}

func (w *plainWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *plainWriter) Write(p []byte) (int, error) { return len(p), nil }

func (w *plainWriter) WriteHeader(status int) { w.status = status }

// flushWriter is a plainWriter that also counts Flush calls.
type flushWriter struct {
	plainWriter
	flushes int
}

func (w *flushWriter) Flush() { w.flushes++ }

func TestStatusRecorderFlush(t *testing.T) {
	tests := []struct {
		name    string
		writer  http.ResponseWriter
		flushes int
	}{
		{
			name:    "forwards to a flushable writer",
			writer:  &flushWriter{},
			flushes: 1,
		},
		{
			name:    "does nothing when the writer cannot flush",
			writer:  &plainWriter{},
			flushes: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &statusRecorder{ResponseWriter: tc.writer}
			assert.NotPanics(t, rec.Flush)

			if fw, ok := tc.writer.(*flushWriter); ok {
				assert.Equal(t, tc.flushes, fw.flushes)
			}
		})
	}
}

func TestHandlerServesMetrics(t *testing.T) {
	m := New()
	m.RecordPoll(true, 5*time.Millisecond)
	m.SetLag(3)
	m.RecordEvents(2)

	srv := httptest.NewServer(m.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), "sorovista_polls_total")
	assert.Contains(t, string(body), `outcome="ok"`)
	assert.Contains(t, string(body), "sorovista_ingest_lag_ledgers 3")
	assert.Contains(t, string(body), "sorovista_events_ingested_total 2")
}

// brokenCollector reports an invalid metric, forcing the registry to return a
// gather error.
type brokenCollector struct{}

func (brokenCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- prometheus.NewDesc("sorovista_broken", "A deliberately broken metric.", nil, nil)
}

func (brokenCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.NewInvalidMetric(
		prometheus.NewDesc("sorovista_broken", "A deliberately broken metric.", nil, nil),
		errors.New("collector exploded"),
	)
}

func TestHandlerReportsGatherErrors(t *testing.T) {
	m := New()
	m.registry.MustRegister(brokenCollector{})

	srv := httptest.NewServer(m.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Contains(t, string(body), "collector exploded",
		"expected the gather error in the response body")
}
