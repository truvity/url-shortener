package urls

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/truvity/policy/examples/url-shortener/internal/business/store"
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

// What a Create request is told, for every combination of what already
// exists. Each row is a sentence a caller reads, so each is stated as one.
//
// The first two are the ones that failed in production: shortening a URL a
// second time. The row was already in the table -- PutURL stores nothing for
// a URL it has seen -- and Create read back the key it had just generated,
// found nothing, dereferenced it, and the caller was told a stream had been
// closed.
func TestWhatACreateIsToldGivenWhatAlreadyExists(t *testing.T) {
	now := time.Now()
	live := &store.URLInfo{URLKey: "abc12345", LongURL: "https://example.com/x"}
	retired := &store.URLInfo{URLKey: "abc12345", LongURL: "https://example.com/x", DeletedAt: &now}
	other := &store.URLInfo{URLKey: "zzz99999", LongURL: "https://example.com/other"}

	for _, c := range []struct {
		name      string
		key       string
		byURL     *store.URLInfo
		keyOwner  *store.URLInfo
		wantReuse *store.URLInfo
		wantCode  connect.Code
	}{
		{"a new URL with no key: go ahead and insert", "", nil, nil, nil, 0},
		{"a new URL with a free key: go ahead and insert", "free1234", nil, nil, nil, 0},
		{"the same URL again with no key is the same link, not an error", "", live, nil, live, 0},
		{"the same URL again with the SAME key is the same link", "abc12345", live, nil, live, 0},
		{"the same URL with a DIFFERENT key is a conflict, and says which key it has", "other000", live, nil, nil, connect.CodeAlreadyExists},
		{"a retired URL cannot be shortened again, and is pointed at Restore", "", retired, nil, nil, connect.CodeFailedPrecondition},
		{"a retired URL asked for by its own key is still retired", "abc12345", retired, nil, nil, connect.CodeFailedPrecondition},
		{"a new URL whose key is taken is a conflict, not an internal error", "zzz99999", nil, other, nil, connect.CodeAlreadyExists},
	} {
		t.Run(c.name, func(t *testing.T) {
			reuse, err := decideCreate(c.key, c.byURL, c.keyOwner)

			if c.wantCode != 0 {
				if got := connect.CodeOf(err); got != c.wantCode {
					t.Fatalf("code = %v (err %v), want %v", got, err, c.wantCode)
				}
				if reuse != nil {
					t.Errorf("a refusal also returned an entry to answer with: %v", reuse)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if reuse != c.wantReuse {
				t.Errorf("reuse = %v, want %v", reuse, c.wantReuse)
			}
		})
	}
}

// A refusal for a URL that already has a key names that key. "Already
// shortened" with no answer to "as what" sends the caller off to look for it,
// and the service is the one thing that knows.
func TestTheConflictNamesTheKeyTheURLAlreadyHas(t *testing.T) {
	existing := &store.URLInfo{URLKey: "abc12345"}

	_, err := decideCreate("other000", existing, nil)

	if err == nil || !strings.Contains(err.Error(), "abc12345") {
		t.Errorf("the refusal does not name the existing key: %v", err)
	}
}
