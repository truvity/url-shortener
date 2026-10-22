// Command compose-chart-schema writes the values schema of every service chart
// under a directory: for each `values.schema.src.json` it finds, the composed
// `values.schema.json` beside it (see package chartschema).
//
// With -check it writes nothing and fails when a committed schema is not what
// its source composes to, which is the drift check the gate runs.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/truvity/policy/chartschema"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "compose-chart-schema:", err)
		os.Exit(1)
	}
}

func run() error {
	check := flag.Bool("check", false, "write nothing; fail when a committed schema is not the composed one")
	flag.Parse()

	if flag.NArg() == 0 {
		return errors.New("usage: compose-chart-schema [-check] dir [dir ...]")
	}

	var stale []string
	for _, dir := range flag.Args() {
		fsys := os.DirFS(dir)
		err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || d.Name() != chartschema.SourceName {
				return nil
			}

			composed, err := chartschema.Compose(fsys, p)
			if err != nil {
				return err
			}

			target := filepath.Join(dir, filepath.Dir(p), "values.schema.json")
			if *check {
				have, err := os.ReadFile(target) //nolint:gosec // a path found by walking the directory the caller named
				if err != nil || !bytes.Equal(have, composed) {
					stale = append(stale, target)
				}

				return nil
			}

			//nolint:gosec // a committed, world-readable chart file
			return os.WriteFile(target, composed, 0o644)
		})
		if err != nil {
			return err
		}
	}

	if len(stale) > 0 {
		return fmt.Errorf("not the composed schema (run `just chart-schemas`): %v", stale)
	}

	return nil
}
