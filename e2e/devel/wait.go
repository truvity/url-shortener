package devel

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// publicReadyTimeout bounds the wait for the release's host to answer.
// helm --wait returns when the pods are ready, which is earlier than the
// gateway programming the route and far earlier than a CDN edge answering for
// a name it has never seen; the first request of a suite must not be what
// discovers that.
const publicReadyTimeout = 5 * time.Minute

// readyPath is a well-formed key that does not exist: the redirect service
// answers it with its own 404, which proves the DNS name, the edge, the
// gateway's JWT policy (the request carries a token), the route and the
// redirect service, and not the database (the handler answers 404 for any
// lookup error).
const readyPath = "/r/_health_"

// WaitPublic waits until the release's host answers the redirect service's
// own 404 for a key that does not exist.
func WaitPublic(ctx context.Context, base string, tokens *TokenSource) error {
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	target := strings.TrimSuffix(base, "/") + readyPath
	deadline := time.Now().Add(publicReadyTimeout)

	var last string

	for {
		token, err := tokens.Token(ctx)
		if err != nil {
			return err
		}

		status, err := get(ctx, client, target, token)
		if err == nil && status == http.StatusNotFound {
			return nil
		}

		last = fmt.Sprintf("status %d", status)
		if err != nil {
			last = err.Error()
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("%s not ready after %s: %s", target, publicReadyTimeout, last)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func get(ctx context.Context, client *http.Client, target, token string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	if err != nil {
		return 0, err
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}

	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode, nil
}
