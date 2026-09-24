package stat

import (
	"context"
	"fmt"
	"net/http"

	"connectrpc.com/connect"

	v1 "github.com/truvity/policy/examples/url-shortener/internal/gen/urlshortener/v1"
	"github.com/truvity/policy/examples/url-shortener/internal/gen/urlshortener/v1/urlshortenerv1connect"
)

// Remote counts a click by ASKING the service that owns the table, rather
// than by writing it.
//
// It satisfies ClickCounter, which is what makes this a five-line change at
// the call site: the interface was already declared here, at the consumer,
// describing the one thing this package needs. An interface declared next to
// the implementation instead would have carried the store's whole surface
// and this type could not have satisfied it.
//
// The counter now holds no database credential at all. That is the point of
// the boundary and not a side effect — a component that cannot write the
// table cannot write it wrongly, and the rights it was granted stop being a
// thing anyone has to reason about.
type Remote struct {
	client urlshortenerv1connect.UrlsServiceClient
}

// NewRemote returns a counter that calls the URL service at addr.
//
// The client speaks gRPC, which needs HTTP/2. Without an identity that means
// HTTP/2 in cleartext, which the transport has to be told to do — see
// runtime.RPCClient, which exists because the default client silently speaks
// HTTP/1.1 instead and the failure arrives as a protocol error on the first
// call rather than as anything about configuration. With an identity,
// HTTP/2 is negotiated by ALPN instead and the same call needs no special
// handling at all.
func NewRemote(httpClient connect.HTTPClient, addr string) *Remote {
	return &Remote{
		client: urlshortenerv1connect.NewUrlsServiceClient(
			httpClient, addr, connect.WithGRPC(),
		),
	}
}

// IncrementClickCount records one click against a long URL.
func (r *Remote) IncrementClickCount(ctx context.Context, longURL string) error {
	_, err := r.client.RecordClick(ctx, connect.NewRequest(&v1.RecordClickRequest{
		LongUrl: longURL,
	}))
	if err != nil {
		// The code is kept, because the caller acts on it: a message the
		// service refused as invalid will be refused again on every
		// redelivery, and one it failed to store will not.
		return fmt.Errorf("record click: %w", err)
	}
	return nil
}

var _ ClickCounter = (*Remote)(nil)
var _ connect.HTTPClient = (*http.Client)(nil)
