package main

import (
	"net/url"
	"testing"
)

// A URL that asks the driver to verify the server carries its root file in
// the query. injectPassword must leave the query alone: pgx reads
// `sslrootcert` from it, and a rewrite that dropped or re-encoded it would
// turn verify-full into a connection that fails, or worse, one that does not.
func TestInjectPasswordKeepsTheVerifyFullQuery(t *testing.T) {
	const raw = "postgres://app@pg-rw.shop.svc.cluster.example:5432/db?sslmode=verify-full&sslrootcert=/etc/url-shortener-pg-ca/ca-certificates.crt"

	got, err := injectPassword(raw, "p@ss/word")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("sslmode") != "verify-full" || q.Get("sslrootcert") != "/etc/url-shortener-pg-ca/ca-certificates.crt" {
		t.Errorf("the TLS query did not survive: %s", u.RawQuery)
	}
	if u.Host != "pg-rw.shop.svc.cluster.example:5432" {
		t.Errorf("the host moved: %s", u.Host)
	}
}
