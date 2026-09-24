package urls

import "testing"

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
