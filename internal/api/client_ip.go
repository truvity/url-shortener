package api

import (
	"net"
	"strings"
)

// ClientIP resolves the originating client address of a request.
//
// Behind a proxy the transport-level address is the proxy's — in-cluster every
// redirect arrives through the Envoy gateway — so the first hop of
// X-Forwarded-For is the real client. A blank or malformed header leaves
// remoteAddr in place.
//
// The trim happens BEFORE the emptiness test on purpose. A header of "   " or
// ", 10.0.0.1" carries no usable first hop, and the two call sites used to
// disagree about exactly that: the request middleware tested the raw header and
// then trimmed, so it replaced the address with "", while the redirect route
// trimmed first and kept it. One request could therefore report two different
// client IPs across its URLRequest and URLRedirect events.
//
// The hop must parse as an address to be used. The header is client-controlled,
// so an unparseable one ("unknown", or anything a caller cares to send) would
// otherwise land in client_ip verbatim and pollute the events; the transport
// address is at least attributable. A hop carrying a port is still a real
// client, so the port is stripped rather than the value discarded.
func ClientIP(xForwardedFor, remoteAddr string) string {
	// Split never returns an empty slice, so index 0 is always safe.
	hop := strings.TrimSpace(strings.Split(xForwardedFor, ",")[0])
	if hop == "" {
		return remoteAddr
	}

	if net.ParseIP(hop) != nil {
		return hop
	}

	if host, _, err := net.SplitHostPort(hop); err == nil && net.ParseIP(host) != nil {
		return host
	}

	return remoteAddr
}
