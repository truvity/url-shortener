package charts_test

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

// assertRestricted fails for every workload in a render that would not be
// admitted under the Pod Security `restricted` profile: a pod without a
// RuntimeDefault seccomp profile or without runAsNonRoot, or a container
// that can escalate privilege or keeps a capability.
//
// It checks the rendered manifest, not a cluster, so it needs nothing the
// gate does not already have. Pods that carry no security context at all are
// exactly what it exists to catch.
func assertRestricted(t *testing.T, rendered string) {
	t.Helper()

	dec := yaml.NewDecoder(bytes.NewReader([]byte(rendered)))
	var pods int

	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode: %v", err)
		}

		kind, _ := doc["kind"].(string)
		if kind != "Deployment" && kind != "Job" && kind != "StatefulSet" && kind != "DaemonSet" {
			continue
		}

		name := dig(doc, "metadata", "name")
		spec, _ := dig(doc, "spec", "template", "spec").(map[string]any)
		if spec == nil {
			t.Errorf("%s %v: no pod spec", kind, name)
			continue
		}
		pods++

		psc, _ := spec["securityContext"].(map[string]any)
		if dig(psc, "runAsNonRoot") != true {
			t.Errorf("%s %v: pod securityContext.runAsNonRoot is not true", kind, name)
		}
		if dig(psc, "seccompProfile", "type") != "RuntimeDefault" {
			t.Errorf("%s %v: pod securityContext.seccompProfile is not RuntimeDefault", kind, name)
		}

		for _, key := range []string{"initContainers", "containers"} {
			list, _ := spec[key].([]any)
			for _, c := range list {
				c, _ := c.(map[string]any)
				csc, _ := c["securityContext"].(map[string]any)
				if dig(csc, "allowPrivilegeEscalation") != false {
					t.Errorf("%s %v container %v: allowPrivilegeEscalation is not false", kind, name, c["name"])
				}
				drop, _ := dig(csc, "capabilities", "drop").([]any)
				if len(drop) != 1 || drop[0] != "ALL" {
					t.Errorf("%s %v container %v: capabilities.drop is not [ALL]", kind, name, c["name"])
				}
			}
		}
	}

	if pods == 0 {
		t.Fatal("the render holds no workload to check")
	}
}

// dig reads a nested value, or nil where any step is missing.
func dig(m any, path ...string) any {
	for _, p := range path {
		mm, ok := m.(map[string]any)
		if !ok {
			return nil
		}
		m = mm[p]
	}
	return m
}

// Every workload of the application chart and of the end-to-end chart —
// services, migration, Job and prober — meets `restricted`, with every
// optional workload switched on.
func TestEveryWorkloadMeetsPodSecurityRestricted(t *testing.T) {
	t.Run("application", func(t *testing.T) {
		out, err := render(t, "-f", filepath.Join("testdata", "everything.yaml"))
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		assertRestricted(t, out)
	})

	t.Run("e2e", func(t *testing.T) {
		out, err := renderE2E(t, "-f", filepath.Join("testdata", "e2e-everything.yaml"))
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		assertRestricted(t, out)
	})
}
