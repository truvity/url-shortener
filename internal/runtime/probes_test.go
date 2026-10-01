package runtime_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/truvity/policy/examples/url-shortener/internal/runtime"
)

// While a dependency is away the process is alive and not ready: liveness
// must keep answering, or the orchestrator restarts a process that is waiting
// exactly as it should.
func TestNotReadyIsNotDead(t *testing.T) {
	connected := false
	srv := runtime.Probes("", func(context.Context) error {
		if !connected {
			return errors.New("database: not connected yet")
		}
		return nil
	})

	get := func(path string) int {
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code
	}

	require.Equal(t, http.StatusOK, get("/health/live"))
	require.Equal(t, http.StatusServiceUnavailable, get("/health/ready"))
	connected = true
	require.Equal(t, http.StatusOK, get("/health/ready"))
}
