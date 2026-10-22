package charts_test

import (
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"
)

// policyDir is the directory of the github.com/truvity/policy module this
// build uses, from the module cache: the contracts this repository is held to
// (docs/contracts) and the fixtures its loaders are tested against
// (config/testdata) are read from there, at the version go.mod pins, rather
// than from a copy kept here.
func policyDir(t *testing.T) string {
	t.Helper()

	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/truvity/policy").Output()
	if err != nil {
		t.Fatalf("go list -m github.com/truvity/policy: %v", err)
	}

	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatal("go list -m github.com/truvity/policy printed no directory")
	}

	return dir
}

// policyModuleVersion is the version of the policy Go module in go.mod,
// without the leading "v": the version the library chart `service-lib` is
// published under.
func policyModuleVersion(t *testing.T) string {
	t.Helper()

	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("no build info: cannot read the policy module's version")
	}

	for _, dep := range info.Deps {
		if dep.Path == "github.com/truvity/policy" {
			return strings.TrimPrefix(dep.Version, "v")
		}
	}

	t.Fatal("github.com/truvity/policy is not a dependency of this build")

	return ""
}
