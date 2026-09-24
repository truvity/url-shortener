package main

import (
	"fmt"
	"net/url"
)

// injectPassword puts a password into a connection URL that was written
// without one.
//
// Parsing rather than string-joining: a password is arbitrary bytes, and
// concatenating one into a URL turns a `@` or a `/` in it into a different
// host or a different database. The failure is silent and points at the
// wrong thing.
func injectPassword(raw, password string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("the database URL is not a URL: %w", err)
	}
	if u.User == nil {
		return "", fmt.Errorf("the database URL names no user, so there is nobody for the password to belong to")
	}
	u.User = url.UserPassword(u.User.Username(), password)
	return u.String(), nil
}
