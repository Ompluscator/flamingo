package filter

import (
	"context"
	"net/http"

	"flamingo.me/flamingo/v3/framework/web"
)

type (
	// MetricsFilter collects status codes of HTTP responses
	MetricsFilter struct{}

	responseWriterMetrics struct {
		rw     http.ResponseWriter
		status int
		bytes  int64
	}

	responseMetrics struct {
		result web.Result
	}
)

// Header to access the response writers Header
func (r *responseWriterMetrics) Header() http.Header {
	return r.rw.Header()
}

// Write to the response writer
func (r *responseWriterMetrics) Write(b []byte) (int, error) {
	written, err := r.rw.Write(b)
	r.bytes += int64(written)
	return written, err
}

// WriteHeader records the status
func (r *responseWriterMetrics) WriteHeader(statusCode int) {
	r.status = statusCode
	r.rw.WriteHeader(statusCode)
}

// Apply metricsFilter to request
func (r responseMetrics) Apply(ctx context.Context, rw http.ResponseWriter) error {
	var err error

	// http.StatusOK is the default case
	responseWriter := &responseWriterMetrics{rw: rw, status: http.StatusOK}

	if r.result != nil {
		err = r.result.Apply(ctx, responseWriter)
	}

	return err
}

// Filter a web request
func (r *MetricsFilter) Filter(ctx context.Context, req *web.Request, w http.ResponseWriter, chain *web.FilterChain) web.Result {
	return &responseMetrics{result: chain.Next(ctx, req, w)}
}
