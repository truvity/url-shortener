package redirect

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/truvity/policy/examples/url-shortener/internal/api"
)

// RegisterHumaRoutes registers all Huma API routes for the redirect component
// This route is PUBLIC (no auth) to allow anonymous redirects
//
// Only the URLRequest middleware is applied here. URLRedirect events are
// emitted by Manager.RedirectWithInfo — it is the SINGLE producer of that
// event. A second producer used to run as Huma middleware over the same
// publisher, so every redirect published URLRedirect twice and the stat
// consumer counted one click twice (IncrementClickCount runs per event).
func RegisterHumaRoutes(
	ctx context.Context,
	logger *slog.Logger,
	humaAPI huma.API,
	manager *Manager,
	urlRequestMiddleware func(huma.Context, func(huma.Context)),
) {
	// URLRequest middleware logs all requests (including redirects)
	humaAPI.UseMiddleware(urlRequestMiddleware)

	RegisterRedirectRoute(ctx, logger, humaAPI, manager)
}

// RegisterRedirectRoute registers GET /r/{url_key} - Redirect to long URL
func RegisterRedirectRoute(ctx context.Context, logger *slog.Logger, humaAPI huma.API, manager *Manager) {
	huma.Register(humaAPI, huma.Operation{
		OperationID:   api.OpRedirectURL,
		Method:        http.MethodGet,
		Path:          api.PathRedirect + api.PathSuffixRedirect,
		Summary:       "Redirect short URL",
		Description:   "Redirects to the original long URL and emits events for statistics",
		Tags:          []string{"URLs"},
		Security:      []map[string][]string{}, // ← PUBLIC (no auth)
		DefaultStatus: http.StatusFound,
	}, func(ctx context.Context, input *api.RedirectURLInput) (*api.RedirectURLOutput, error) {
		startTime := time.Now()

		// Extract the transport-level address from the Huma context (if
		// available); api.ClientIP prefers X-Forwarded-For over it. The
		// retired URLRedirect middleware did the same — keep the fidelity
		// now that the manager is the only emitter.
		remoteAddr := ""
		if humaCtxVal := ctx.Value("huma.context"); humaCtxVal != nil {
			if humaCtx, ok := humaCtxVal.(huma.Context); ok {
				remoteAddr = humaCtx.RemoteAddr()
			}
		}

		clientIP := api.ClientIP(input.XForwardedFor, remoteAddr)

		// Build request info for event emission
		reqInfo := RequestInfo{
			ClientIP:  clientIP,
			UserAgent: input.UserAgent,
			Referer:   input.Referer,
			StartTime: startTime,
		}

		// Call business logic (emits events internally)
		longURL, err := manager.RedirectWithInfo(ctx, input.URLKey, reqInfo)
		if err != nil {
			return nil, huma.Error404NotFound("URL not found", err)
		}

		return &api.RedirectURLOutput{
			Location: longURL,
		}, nil
	})
}
