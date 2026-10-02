package charts_test

import (
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/truvity/policy/examples/url-shortener/charts"

	yaml "go.yaml.in/yaml/v3"
)

// annotation is the Chart.yaml annotation a chart declares its delivery
// interface under (docs/contracts/delivery-interface.md).
const annotation = "delivery.truvity.io/interface"

// registrySteps reads the step numbers of the registry's section 3 table out
// of the contract itself, so the number a chart declares is held to a step
// that exists rather than to a constant that can drift from the document.
func registrySteps(t *testing.T) map[int]bool {
	t.Helper()

	raw, err := os.ReadFile("../../../docs/contracts/delivery-interface.md")
	if err != nil {
		t.Fatal(err)
	}

	steps := map[int]bool{}

	for _, m := range regexp.MustCompile(`(?m)^\| (\d+) \| `).FindAllStringSubmatch(string(raw), -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}

		steps[n] = true
	}

	if len(steps) == 0 {
		t.Fatal("docs/contracts/delivery-interface.md has no step table")
	}

	// Steps are numbered from 1 with no gap: a gap is a step somebody
	// removed, which the contract says never happens.
	for n := 1; n <= len(steps); n++ {
		if !steps[n] {
			t.Fatalf("the registry's steps are not 1..%d without a gap: %v", len(steps), steps)
		}
	}

	return steps
}

// TestEveryChartDeclaresTheSameDeliveryInterface holds the three charts of
// the product to the registry's rules 2 and 3: each carries the annotation,
// as one whole number, naming a step the registry lists, and the number is
// the same on all three because they are released and pinned together.
func TestEveryChartDeclaresTheSameDeliveryInterface(t *testing.T) {
	t.Parallel()

	steps := registrySteps(t)

	declared := map[string]int{}

	for _, chart := range []string{"url-shortener", "url-shortener-infra", "url-shortener-e2e"} {
		raw, err := charts.Files.ReadFile(chart + "/Chart.yaml")
		if err != nil {
			t.Fatal(err)
		}

		var meta struct {
			Annotations map[string]string `yaml:"annotations"`
		}
		if err := yaml.Unmarshal(raw, &meta); err != nil {
			t.Fatalf("%s/Chart.yaml: %v", chart, err)
		}

		value, ok := meta.Annotations[annotation]
		if !ok {
			t.Errorf("%s/Chart.yaml has no %s annotation", chart, annotation)

			continue
		}

		n, err := strconv.Atoi(value)
		if err != nil || strconv.Itoa(n) != value {
			t.Errorf("%s/Chart.yaml: %s is %q, want a whole number", chart, annotation, value)

			continue
		}

		if !steps[n] {
			t.Errorf("%s/Chart.yaml: %s is %d, which the registry has no step for", chart, annotation, n)
		}

		declared[chart] = n
	}

	first := -1

	for chart, n := range declared {
		if first == -1 {
			first = n
		}

		if n != first {
			t.Errorf("%s declares interface %d but another chart of the product declares %d: the three are released and pinned together, so they carry one number", chart, n, first)
		}
	}
}
