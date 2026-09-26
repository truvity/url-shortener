package suite

import (
	"context"
	"testing"
	"time"
)

// TestHealthEndpoints proves /health/live and /health/ready answer through
// the Service, on the probe port the chart's own liveness and readiness
// probes use — the same question the kubelet already asks the Pod
// directly, asked here through the boundary a caller would actually reach.
//
// stat and log carry no Service of their own (they are pure consumers —
// see charts/url-shortener/templates/{stat,log}.yaml); this suite proves
// they are alive the way it proves everything else about them, through
// their EFFECTS (TestStatMovesTheCounter, TestLogArchivesTheRecord).
func TestHealthEndpoints(t *testing.T) {
	for _, component := range []string{componentURLs, componentRedirect, componentWeb} {
		t.Run(component, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			probeHealth(ctx, t, component)
		})
	}
}
