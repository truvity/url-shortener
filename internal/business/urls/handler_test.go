package urls

import (
	"strings"
	"testing"
)

// A caller cannot raise the page ceiling, and asking to is not an error.
//
// The ceiling belongs to the service: it is what keeps one request from
// reading a table that grew after this was written. Refusing an oversized
// ask would push that number into every client, where it would go stale;
// clamping keeps it in one place and still answers.
//
// Zero means "no preference", not "none" — a caller that omits the field
// gets the default rather than an empty page, which is the shape a
// generated client produces when it does not set it at all.
func TestThePageSizeCeilingIsTheService(t *testing.T) {
	for _, c := range []struct {
		name string
		ask  int
		want int
	}{
		{"omitted means the default", 0, DefaultPageSize},
		{"negative means the default", -1, DefaultPageSize},
		{"a modest ask is honoured", 10, 10},
		{"the ceiling is the ceiling", MaxPageSize, MaxPageSize},
		{"over it is clamped, not refused", MaxPageSize * 10, MaxPageSize},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := pageSize(c.ask); got != c.want {
				t.Errorf("pageSize(%d) = %d, want %d", c.ask, got, c.want)
			}
		})
	}
}

// A generated key is the shape validKey demands, drawn from the alphabet
// the front end tells a caller a key looks like -- a generated key that
// failed its OWN service's validation would be a bug no caller could work
// around.
func TestARandomKeyIsTheShapeTheServiceAccepts(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		key, err := randomKey()
		if err != nil {
			t.Fatalf("randomKey: %v", err)
		}
		if err := validKey(key); err != nil {
			t.Fatalf("a generated key failed the service's own check: %v (key %q)", err, key)
		}
		for _, r := range key {
			if !strings.ContainsRune(keyAlphabet, r) {
				t.Fatalf("key %q contains %q, outside the declared alphabet", key, r)
			}
		}
		seen[key] = true
	}
	// Not a statistical proof of randomness -- a sanity check that this
	// draws from crypto/rand at all rather than, say, returning a
	// constant that happens to be valid.
	if len(seen) < 95 {
		t.Errorf("100 draws produced only %d distinct keys", len(seen))
	}
}
