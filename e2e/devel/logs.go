package devel

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// ErrNotArchivedYet is ArchivedLine's answer while the archiver has not
// written an object for this release.
var ErrNotArchivedYet = errors.New("no `archived` line with records under this release's prefix yet")

// ArchivedLine reports an error unless the archiver's log holds an `archived`
// line naming an object under prefix and at least one record. The archiver
// writes it only after the object was put, which is the claim the suite needs
// and cannot read from the bucket (not a door the suite has).
func ArchivedLine(logs, prefix string) error {
	lines := strings.Split(logs, "\n")
	slices.Reverse(lines)

	for _, line := range lines {
		var rec struct {
			Event   string `json:"event"`
			Msg     string `json:"msg"`
			Key     string `json:"key"`
			Records int    `json:"records"`
		}

		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}

		if rec.Event != "archived" && rec.Msg != "archived" {
			continue
		}

		if rec.Records >= 1 && strings.HasPrefix(rec.Key, prefix) {
			return nil
		}
	}

	return ErrNotArchivedYet
}
