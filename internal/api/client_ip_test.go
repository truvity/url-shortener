package api_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/truvity/policy/examples/url-shortener/internal/api"
)

func TestClientIP(t *testing.T) {
	const gateway = "10.0.0.1" // what RemoteAddr reports in-cluster: the Envoy gateway

	tests := []struct {
		name          string
		xForwardedFor string
		remoteAddr    string
		want          string
	}{
		{
			name:       "no header falls back to the transport address",
			remoteAddr: gateway,
			want:       gateway,
		},
		{
			name:          "single hop is the client",
			xForwardedFor: "203.0.113.5",
			remoteAddr:    gateway,
			want:          "203.0.113.5",
		},
		{
			name:          "first hop wins over later proxies",
			xForwardedFor: "203.0.113.5, 70.41.3.18, 150.172.238.178",
			remoteAddr:    gateway,
			want:          "203.0.113.5",
		},
		{
			name:          "surrounding whitespace is trimmed",
			xForwardedFor: "  203.0.113.5  , 70.41.3.18",
			remoteAddr:    gateway,
			want:          "203.0.113.5",
		},
		// The two cases below are the ones the duplicated implementations
		// disagreed on: the request middleware tested the raw header before
		// trimming, so it overwrote the address with "" and lost the client
		// entirely, while the redirect route kept the transport address.
		{
			name:          "whitespace-only header keeps the transport address",
			xForwardedFor: "   ",
			remoteAddr:    gateway,
			want:          gateway,
		},
		{
			name:          "empty first hop keeps the transport address",
			xForwardedFor: ", 203.0.113.5",
			remoteAddr:    gateway,
			want:          gateway,
		},
		// The header is client-controlled, so an unparseable hop must not reach
		// client_ip — the transport address is at least attributable.
		{
			name:          "an unparseable hop keeps the transport address",
			xForwardedFor: "not-an-address",
			remoteAddr:    gateway,
			want:          gateway,
		},
		{
			name:          `the conventional "unknown" hop keeps the transport address`,
			xForwardedFor: "unknown, 203.0.113.5",
			remoteAddr:    gateway,
			want:          gateway,
		},
		{
			name:          "a hop with a port keeps the address, drops the port",
			xForwardedFor: "203.0.113.5:41234",
			remoteAddr:    gateway,
			want:          "203.0.113.5",
		},
		{
			name:          "IPv6 hop",
			xForwardedFor: "2001:db8::1, 70.41.3.18",
			remoteAddr:    gateway,
			want:          "2001:db8::1",
		},
		{
			name:          "bracketed IPv6 hop with a port",
			xForwardedFor: "[2001:db8::1]:41234",
			remoteAddr:    gateway,
			want:          "2001:db8::1",
		},
		{
			name:          "no usable value anywhere yields empty, not a panic",
			xForwardedFor: "",
			remoteAddr:    "",
			want:          "",
		},
		{
			name:          "header still wins when the transport address is unknown",
			xForwardedFor: "203.0.113.5",
			remoteAddr:    "",
			want:          "203.0.113.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, api.ClientIP(tt.xForwardedFor, tt.remoteAddr))
		})
	}
}
