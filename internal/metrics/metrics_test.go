package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getCounterValue(m prometheus.Counter) float64 {
	var pb dto.Metric
	_ = m.Write(&pb)
	if pb.Counter != nil {
		return pb.Counter.GetValue()
	}
	return 0
}

func getHistogramSampleCount(h prometheus.Observer) uint64 {
	metric, ok := h.(prometheus.Metric)
	if !ok {
		return 0
	}
	var pb dto.Metric
	_ = metric.Write(&pb)
	if pb.Histogram != nil {
		return pb.Histogram.GetSampleCount()
	}
	return 0
}

func TestRecordEvents(t *testing.T) {
	tests := []struct {
		name      string
		metrics   *Metrics
		counts    []int64
		wantTotal float64
	}{
		{
			name:      "happy path records positive event counts",
			metrics:   New(),
			counts:    []int64{5, 10, 1},
			wantTotal: 16,
		},
		{
			name:      "zero count adds nothing",
			metrics:   New(),
			counts:    []int64{0},
			wantTotal: 0,
		},
		{
			name:      "nil metrics receiver is a no-op",
			metrics:   nil,
			counts:    []int64{10, 20},
			wantTotal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, count := range tt.counts {
				// Calling RecordEvents should never panic even if metrics is nil.
				tt.metrics.RecordEvents(count)
			}

			if tt.metrics != nil {
				got := getCounterValue(tt.metrics.eventsIngested)
				assert.Equal(t, tt.wantTotal, got)
			}
		})
	}
}

func TestMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		metrics        *Metrics
		handlerStatus  int
		handlerBody    string
		routePattern   string
		method         string
		path           string
		wantStatus     int
		wantBody       string
		wantObserved   bool
		expectedRoute  string
		expectedStatus string
	}{
		{
			name:           "happy path records status 200 with route pattern",
			metrics:        New(),
			handlerStatus:  http.StatusOK,
			handlerBody:    "ok",
			routePattern:   "/events/{id}",
			method:         http.MethodGet,
			path:           "/events/123",
			wantStatus:     http.StatusOK,
			wantBody:       "ok",
			wantObserved:   true,
			expectedRoute:  "/events/{id}",
			expectedStatus: "200",
		},
		{
			name:           "error path records status 500 when handler fails",
			metrics:        New(),
			handlerStatus:  http.StatusInternalServerError,
			handlerBody:    "internal error",
			routePattern:   "/events",
			method:         http.MethodPost,
			path:           "/events",
			wantStatus:     http.StatusInternalServerError,
			wantBody:       "internal error",
			wantObserved:   true,
			expectedRoute:  "/events",
			expectedStatus: "500",
		},
		{
			name:           "error path records status 404 client error",
			metrics:        New(),
			handlerStatus:  http.StatusNotFound,
			handlerBody:    "not found",
			routePattern:   "",
			method:         http.MethodGet,
			path:           "/not-found",
			wantStatus:     http.StatusNotFound,
			wantBody:       "not found",
			wantObserved:   true,
			expectedRoute:  "/not-found",
			expectedStatus: "404",
		},
		{
			name:          "nil metrics receiver delegates to handler safely without recording",
			metrics:       nil,
			handlerStatus: http.StatusOK,
			handlerBody:   "nil metrics ok",
			routePattern:  "/test",
			method:        http.MethodGet,
			path:          "/test",
			wantStatus:    http.StatusOK,
			wantBody:      "nil metrics ok",
			wantObserved:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origPattern := RoutePattern
			defer func() { RoutePattern = origPattern }()

			RoutePattern = func(r *http.Request) string {
				return tt.routePattern
			}

			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.handlerStatus)
				_, _ = w.Write([]byte(tt.handlerBody))
			})

			wrapped := tt.metrics.Middleware(nextHandler)
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantBody, rec.Body.String())

			if tt.wantObserved && tt.metrics != nil {
				observer, err := tt.metrics.httpDuration.GetMetricWithLabelValues(
					tt.expectedRoute,
					tt.method,
					tt.expectedStatus,
				)
				require.NoError(t, err)
				count := getHistogramSampleCount(observer)
				assert.Equal(t, uint64(1), count)
			}
		})
	}
}
