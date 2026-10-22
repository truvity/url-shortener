package devel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// MintAccessToken asks `sluisctl token` for the pseudo-customer token a
// release's PUBLIC host is called with: the audience `devel:url-shortener` at
// the access issuer.
//
// It is a second exchange, distinct from the `k8s:devel` token the kubeconfig
// runs: the cluster token opens the Kubernetes API and nothing on a release,
// and this one opens a release and nothing in the cluster. In a workflow
// sluisctl exchanges the job's GitHub OIDC token; on a laptop it answers from
// the `sluisctl login` session. The same command, the same arguments.
func MintAccessToken(ctx context.Context, issuer string) (string, error) {
	cmd := exec.CommandContext(ctx, "sluisctl", "token", "--issuer", issuer, "--audience", Audience) //nolint:gosec // fixed argv
	// Stderr stays attached: without a session sluisctl says to run
	// `sluisctl login`, and swallowing it turns that into an exit status.
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("sluisctl token --audience %s: %w", Audience, err)
	}

	// The bare JWT; take the last non-empty line so a banner cannot become
	// the token.
	var token string

	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			token = line
		}
	}

	if strings.Count(token, ".") != 2 {
		return "", fmt.Errorf("sluisctl token --audience %s returned something that is not a JWT", Audience)
	}

	return token, nil
}

// refreshBefore is how long before `exp` a TokenSource stops handing a
// token out: a CI token lives at most 15 minutes (the client's ttl_cap) and
// a run can be longer.
const refreshBefore = 4 * time.Minute

// TokenSource hands out a token and mints a new one when the held one is
// within refreshBefore of expiring. Safe for concurrent use.
type TokenSource struct {
	mint func(context.Context) (string, error)
	now  func() time.Time

	mu     sync.Mutex
	token  string
	expiry time.Time
}

// NewTokenSource is a TokenSource over MintAccessToken.
func NewTokenSource(issuer string) *TokenSource {
	return &TokenSource{
		mint: func(ctx context.Context) (string, error) { return MintAccessToken(ctx, issuer) },
		now:  time.Now,
	}
}

// Token returns a token with more than refreshBefore left, minting if needed.
// A token with no readable exp is used once and not kept.
func (s *TokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token != "" && s.now().Add(refreshBefore).Before(s.expiry) {
		return s.token, nil
	}

	token, err := s.mint(ctx)
	if err != nil {
		return "", err
	}

	expiry, err := tokenExpiry(token)
	if err != nil {
		s.token, s.expiry = "", time.Time{}

		return token, nil
	}

	s.token, s.expiry = token, expiry

	return token, nil
}

// tokenExpiry reads `exp` from a JWT without verifying it: the issuer and the
// gateway verify the signature, this only decides when to ask again.
func tokenExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("not a JWT")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("decode payload: %w", err)
	}

	var claims struct {
		Exp float64 `json:"exp"`
	}

	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("parse claims: %w", err)
	}

	if claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("no exp")
	}

	return time.Unix(int64(claims.Exp), 0), nil
}

func randomPassword() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	out := make([]byte, 32)

	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}

		out[i] = alphabet[n.Int64()]
	}

	return string(out), nil
}

// NewTestTokenSource is a TokenSource over a function and a clock, for the
// tests of the refresh rule.
func NewTestTokenSource(mint func() string, now func() time.Time) *TokenSource {
	return &TokenSource{mint: func(context.Context) (string, error) { return mint(), nil }, now: now}
}
