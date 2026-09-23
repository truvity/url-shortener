package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/truvity/policy/examples/url-shortener/internal/api"
)

func TestNewVersionHandler(t *testing.T) {
	tests := []struct {
		name      string
		component string
		info      *api.Version
		want      api.VersionResponse
	}{
		{
			name:      "release build",
			component: "redirect",
			info:      &api.Version{Version: "1.5.2", Commit: "abc1234"},
			want:      api.VersionResponse{Component: "redirect", Version: "1.5.2", Commit: "abc1234"},
		},
		{
			name:      "dev build (ldflags defaults)",
			component: "redirect",
			info:      &api.Version{Version: "dev", Commit: "unknown"},
			want:      api.VersionResponse{Component: "redirect", Version: "dev", Commit: "unknown"},
		},
		{
			name:      "nil info degrades, never panics",
			component: "redirect",
			info:      nil,
			want:      api.VersionResponse{Component: "redirect", Version: "unknown", Commit: "unknown"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			app.Get(api.PathVersion, api.NewVersionHandler(tt.component, tt.info))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, api.PathVersion, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			var got api.VersionResponse
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
			assert.Equal(t, tt.want, got)
		})
	}
}
