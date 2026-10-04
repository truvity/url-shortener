package charts_test

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

// infraDefaults supplies the one value the infrastructure chart refuses to
// invent: the name of the secret holding the runtime role's password.
func infraDefaults(extra ...string) []string {
	return append([]string{
		"--set", "postgres.runtimePasswordSecret=example-pg-runtime",
	}, extra...)
}

func renderInfra(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("helm", append([]string{"template", "example", chartDir(t, "url-shortener-infra")}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// The infrastructure chart's goldens, on the same terms as the application
// chart's: `minimal` catches a changed default, `everything` catches a
// template that stopped reading a value.
//
// `everything` matters more here than it does next door. Most of this
// chart's values exist for a PLATFORM to set and for nobody else, so they
// are unset in every render an example ever does — which means the only
// thing standing between "the platform passes it" and "the platform passes
// it and it goes nowhere" is a fixture that sets all of them.
//
// Regenerate with `just golden` after reading the diff, never before.
func TestWhatTheInfraChartRenders(t *testing.T) {
	for _, name := range []string{"infra-minimal", "infra-everything", "infra-platform-owned"} {
		t.Run(name, func(t *testing.T) {
			out, err := renderInfra(t, "-f", filepath.Join("testdata", name+".yaml"))
			if err != nil {
				t.Fatalf("the chart does not render: %v\n%s", err, out)
			}

			path := filepath.Join("testdata", "golden", name+".yaml")
			if os.Getenv("UPDATE_GOLDEN") != "" {
				if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Skip("golden updated")
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v — run `just golden` to create it", err)
			}
			if string(want) != out {
				t.Errorf("the render moved. Read the diff, then `just golden`:\n%s",
					firstDifference(string(want), out))
			}
		})
	}
}

// A chart renders what it was given, or it refuses.
//
// An archive with no server name joins whatever timeline is already under
// the default, and nobody finds out until the restore. Rendering something
// here would be worse than failing.
func TestTheInfraChartRefusesAnArchiveItCannotName(t *testing.T) {
	out, err := renderInfra(t, infraDefaults("--set", "postgres.backup.objectStoreName=example-archive")...)
	if err == nil {
		t.Fatalf("an archive was named with no server name and the chart rendered anyway:\n%s", out)
	}
	if !strings.Contains(out, "postgres.backup.serverName") {
		t.Errorf("the refusal does not name postgres.backup.serverName, so nobody reading it knows what to set:\n%s", out)
	}
}

// The platform cannot rename what the chart's own selectors look for.
//
// Labels are the one pass-through here that could collide with something
// load-bearing. A platform stamping `app.kubernetes.io/instance` would take
// the release apart one resource at a time, and every render would look
// correct.
func TestTheChartsLabelsWinOverThePlatforms(t *testing.T) {
	out, err := renderInfra(t, infraDefaults(
		"--set", `postgres.labels.app\.kubernetes\.io/instance=hijacked`,
		"--set", `postgres.labels.example\.io/pool=durable`,
	)...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	cluster := docOfKind(t, out, "Cluster")
	labels, _ := cluster["metadata"].(map[string]any)["labels"].(map[string]any)
	if got := labels["app.kubernetes.io/instance"]; got != "example" {
		t.Errorf("a platform label overwrote the release name: instance = %v, want example", got)
	}
	if got := labels["example.io/pool"]; got != "durable" {
		t.Errorf("the platform's own label did not survive: example.io/pool = %v", got)
	}
}

// An account and a server list are two answers to one question.
//
// Without an account the stream says which broker to dial. With one, the
// account carries both the broker and the identity, and a server list
// beside it is the chart arguing with the broker about who this is — which
// NACK resolves by ignoring one of them, silently, and not always the same
// one.
func TestTheStreamNamesAnAccountOrAServerButNeverBoth(t *testing.T) {
	t.Run("no account", func(t *testing.T) {
		out, err := renderInfra(t, infraDefaults()...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		spec := docOfKind(t, out, "Stream")["spec"].(map[string]any)
		if _, ok := spec["servers"]; !ok {
			t.Error("no account and no servers: the stream is created on nothing")
		}
		if _, ok := spec["account"]; ok {
			t.Error("an account appeared without being asked for")
		}
	})

	t.Run("account", func(t *testing.T) {
		out, err := renderInfra(t, infraDefaults("--set", "events.account=url-shortener")...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		spec := docOfKind(t, out, "Stream")["spec"].(map[string]any)
		if got := spec["account"]; got != "url-shortener" {
			t.Errorf("account = %v, want url-shortener", got)
		}
		if _, ok := spec["servers"]; ok {
			t.Error("a server list was rendered beside the account")
		}
	})
}

// Rule 6 of the platform contract, mechanically: a service FINDS its store,
// it does not make it.
//
// `platform.md` says this is checked by review today and that it should not
// be. This is the check — the application chart renders nothing whose kind
// belongs to the infrastructure one, and the infrastructure chart renders
// nothing that runs.
//
// It is not theoretical. The two charts exist as two because the migration
// runs as a pre-install hook and Helm runs hooks before anything else in
// the same release, so a chart creating its own database could never
// migrate it. That ordering was discovered by building it the other way
// first, and nothing but this test would notice it being undone.
func TestNeitherChartRendersTheOthersKinds(t *testing.T) {
	infrastructure := map[string]bool{"Cluster": true, "Stream": true, "Consumer": true, "Bucket": true}
	running := map[string]bool{"Deployment": true, "StatefulSet": true, "DaemonSet": true, "CronJob": true}

	app, err := render(t, defaults()...)
	if err != nil {
		t.Fatalf("the application chart does not render: %v\n%s", err, app)
	}
	for _, kind := range kindsIn(t, app) {
		if infrastructure[kind] {
			t.Errorf("the application chart renders a %s — it is making what it should be finding", kind)
		}
	}

	infra, err := renderInfra(t, infraDefaults()...)
	if err != nil {
		t.Fatalf("the infrastructure chart does not render: %v\n%s", err, infra)
	}
	for _, kind := range kindsIn(t, infra) {
		if running[kind] {
			t.Errorf("the infrastructure chart renders a %s — the thing with a separate lifetime has grown a workload", kind)
		}
	}
}

// docOfKind returns the one rendered document of a kind, decoded.
func docOfKind(t *testing.T, out, kind string) map[string]any {
	t.Helper()
	var found map[string]any
	for _, m := range documents(t, out) {
		if m["kind"] != kind {
			continue
		}
		if found != nil {
			t.Fatalf("more than one %s was rendered; this helper assumes one", kind)
		}
		found = m
	}
	if found == nil {
		t.Fatalf("no %s in the render:\n%s", kind, out)
	}
	return found
}

// kindsIn lists the kinds a render produced, in the order they appear.
func kindsIn(t *testing.T, out string) []string {
	t.Helper()
	var kinds []string
	for _, m := range documents(t, out) {
		if kind, ok := m["kind"].(string); ok {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) == 0 {
		t.Fatalf("nothing was rendered:\n%s", out)
	}
	return kinds
}

// documents decodes a helm render into its documents.
//
// A decoder rather than a split on `---`: that separator appears inside
// block scalars too, and a test that quietly stopped seeing half the render
// would pass for the wrong reason, which is the only kind of green worth
// worrying about.
func documents(t *testing.T, out string) []map[string]any {
	t.Helper()
	var docs []map[string]any
	dec := yaml.NewDecoder(strings.NewReader(out))
	for {
		var m map[string]any
		err := dec.Decode(&m)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("the render is not YAML: %v\n%s", err, out)
		}
		if m != nil {
			docs = append(docs, m)
		}
	}
	return docs
}

// The managed role declares the fields the API server would otherwise fill
// in for it.
//
// A CRD's schema carries defaults, and the API server applies them on the
// way in. A chart that leaves them out therefore renders a role that never
// matches the one that is stored, and every renderer comparing desired to
// live reports a difference — permanently, on a resource nobody has
// touched. The usual reflex is to tell the comparer to ignore those
// fields, which also hides the real changes underneath them.
//
// Declaring the operator's own defaults changes nothing about the role. It
// only says which values this chart wants, somewhere a reader can see them.
//
// Found on a cluster: a database that was Healthy and OutOfSync at the same
// time, differing on exactly these two keys.
func TestTheManagedRoleDeclaresWhatTheAPIDefaults(t *testing.T) {
	out, err := renderInfra(t, infraDefaults()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	var cluster map[string]any
	for _, doc := range strings.Split(out, "\n---") {
		var m map[string]any
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil || m == nil {
			continue
		}
		if m["kind"] == "Cluster" {
			cluster = m
			break
		}
	}
	if cluster == nil {
		t.Fatal("no Cluster in the render")
	}

	managed, _ := cluster["spec"].(map[string]any)["managed"].(map[string]any)
	roles, _ := managed["roles"].([]any)
	if len(roles) != 1 {
		t.Fatalf("expected one managed role, got %d", len(roles))
	}

	role, _ := roles[0].(map[string]any)
	for _, key := range []string{"connectionLimit", "inherit"} {
		if _, declared := role[key]; !declared {
			t.Errorf("the role leaves %q to the API server, which stores a value the chart never renders — a permanent difference on a role nobody changed", key)
		}
	}
}

// A test install provisions NOTHING of its own.
//
// This is the property that makes one chart serve three kinds of install.
// An engineer's namespace typically grants the built-in `admin` role,
// which covers no custom resources at all — so a chart that mints them
// under every tier is one that engineer cannot install, and the failure
// arrives as a permissions error naming a kind rather than a tier.
//
// It is also what keeps the cloud objects honest in a repository that is
// read far more often than it is deployed: they render only where a
// platform asked for them.
func TestATestInstallMintsNothing(t *testing.T) {
	out, err := renderInfra(t, infraDefaults()...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	for _, kind := range []string{"Bucket", "Policy", "Role", "PodIdentityAssociation", "ApplicationNetworkPolicy"} {
		if strings.Contains(out, "kind: "+kind) {
			t.Errorf("a test install renders a %s; it runs as the namespace's standing identity and mints nothing", kind)
		}
	}
}

// A primary install provisions the objects it owns, and every name is given.
func TestAPrimaryInstallMintsWhatItOwns(t *testing.T) {
	out, err := renderInfra(t, infraDefaults(
		"--set", "tier=primary",
		"--set", "cloud.bucket=a-bucket",
		"--set", "cloud.iamName=an-identity",
		"--set", "cloud.clusterName=a-cluster",
		"--set", "cloud.accountID=example-account-id",
		"--set", "cloud.region=a-region",
		"--set", "cloud.serviceAccount=an-account",
	)...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	for _, kind := range []string{"Bucket", "Policy", "Role", "PodIdentityAssociation", "ApplicationNetworkPolicy"} {
		if !strings.Contains(out, "kind: "+kind) {
			t.Errorf("a primary install renders no %s", kind)
		}
	}

	// ListBucket on the BUCKET, with no trailing /*. Granted on the
	// contents it reads as a permission and authorises nothing: the
	// writer's start-up check 403s against a policy that looks right in
	// every review. Found exactly that way.
	if !strings.Contains(out, `"Resource": "arn:aws:s3:::a-bucket"`) {
		t.Error("ListBucket is not granted on the bucket itself, so HeadBucket is refused")
	}
	// And it still cannot read or purge a single record.
	for _, forbidden := range []string{"s3:GetObject", "s3:DeleteObject", "s3:DeleteBucket"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("the archive role is granted %s; it appends and nothing more", forbidden)
		}
	}
}

// tagValue returns the value for a key in an ACK list-of-{key,value} tag
// field — Role, Policy and the Bucket's own tagging.tagSet all take this
// shape — or "" with ok=false if the key is absent.
func tagValue(tags []any, key string) (string, bool) {
	for _, raw := range tags {
		tag, _ := raw.(map[string]any)
		if tag["key"] == key {
			v, _ := tag["value"].(string)
			return v, true
		}
	}
	return "", false
}

// A platform's permissions boundary on the controller that creates these
// resources can condition creation on specific request tags being
// present — found on an install whose boundary denied iam:CreateRole
// because the Role carried `cluster` but not `project`, which left the
// PodIdentityAssociation pointing at a Role that was never created. This
// is the property the boundary depends on: both tags land on the Role,
// the Policy, the Bucket's own tagSet and the PodIdentityAssociation, on
// the same terms.
func TestCloudResourcesCarryClusterAndProjectTags(t *testing.T) {
	args := infraDefaults(
		"--set", "tier=primary",
		"--set", "cloud.bucket=a-bucket",
		"--set", "cloud.iamName=an-identity",
		"--set", "cloud.clusterName=a-cluster",
		"--set", "cloud.accountID=example-account-id",
		"--set", "cloud.region=a-region",
		"--set", "cloud.serviceAccount=an-account",
	)

	t.Run("default project is the release namespace", func(t *testing.T) {
		out, err := renderInfra(t, args...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}

		role := docOfKind(t, out, "Role")["spec"].(map[string]any)
		roleTags, _ := role["tags"].([]any)
		if v, ok := tagValue(roleTags, "cluster"); !ok || v != "a-cluster" {
			t.Errorf("Role cluster tag = %q, %v; want a-cluster, true", v, ok)
		}
		if v, ok := tagValue(roleTags, "project"); !ok || v != "default" {
			t.Errorf("Role project tag = %q, %v; want the release namespace (default), true", v, ok)
		}

		policy := docOfKind(t, out, "Policy")["spec"].(map[string]any)
		policyTags, _ := policy["tags"].([]any)
		if _, ok := tagValue(policyTags, "cluster"); !ok {
			t.Error("Policy has no cluster tag")
		}
		if _, ok := tagValue(policyTags, "project"); !ok {
			t.Error("Policy has no project tag")
		}

		bucket := docOfKind(t, out, "Bucket")["spec"].(map[string]any)
		tagging, _ := bucket["tagging"].(map[string]any)
		bucketTags, _ := tagging["tagSet"].([]any)
		if _, ok := tagValue(bucketTags, "cluster"); !ok {
			t.Error("Bucket tagSet has no cluster tag")
		}
		if _, ok := tagValue(bucketTags, "project"); !ok {
			t.Error("Bucket tagSet has no project tag")
		}
		// The bucket's own fixed tags survive alongside the new ones.
		if _, ok := tagValue(bucketTags, "truvity.io/preserve"); !ok {
			t.Error("Bucket lost its own truvity.io/preserve tag")
		}

		pia := docOfKind(t, out, "PodIdentityAssociation")["spec"].(map[string]any)
		piaTags, _ := pia["tags"].(map[string]any)
		if piaTags["cluster"] != "a-cluster" {
			t.Errorf("PodIdentityAssociation cluster tag = %v, want a-cluster", piaTags["cluster"])
		}
		if piaTags["project"] != "default" {
			t.Errorf("PodIdentityAssociation project tag = %v, want the release namespace (default)", piaTags["project"])
		}
	})

	t.Run("cloud.project overrides the namespace", func(t *testing.T) {
		out, err := renderInfra(t, append(append([]string{}, args...), "--set", "cloud.project=payments")...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		role := docOfKind(t, out, "Role")["spec"].(map[string]any)
		roleTags, _ := role["tags"].([]any)
		if v, _ := tagValue(roleTags, "project"); v != "payments" {
			t.Errorf("Role project tag = %q, want payments", v)
		}
	})

	t.Run("cloud.tags cannot override cluster or project", func(t *testing.T) {
		out, err := renderInfra(t, append(append([]string{}, args...),
			"--set", "cloud.tags.cluster=hijacked",
			"--set", "cloud.tags.project=hijacked",
			"--set", "cloud.tags.team=payments",
		)...)
		if err != nil {
			t.Fatalf("the chart does not render: %v\n%s", err, out)
		}
		role := docOfKind(t, out, "Role")["spec"].(map[string]any)
		roleTags, _ := role["tags"].([]any)
		if v, _ := tagValue(roleTags, "cluster"); v != "a-cluster" {
			t.Errorf("cloud.tags overrode the cluster tag: got %q", v)
		}
		if v, _ := tagValue(roleTags, "project"); v != "default" {
			t.Errorf("cloud.tags overrode the project tag: got %q", v)
		}
		if v, ok := tagValue(roleTags, "team"); !ok || v != "payments" {
			t.Errorf("cloud.tags' own extra tag did not survive: team = %q, %v", v, ok)
		}
	})
}

// The name is required whether or not the chart is asked to fill it.
//
// generate decides WHO writes the secret, not what it is called -- the
// chart still does not invent a name any more than it invents a value,
// and a bare install saying nothing at all is told so.
func TestThePasswordNameIsRequiredEitherWay(t *testing.T) {
	out, err := renderInfra(t)
	if err == nil {
		t.Fatal("the chart rendered with no password secret name at all")
	}
	if !strings.Contains(out, "runtimePasswordSecret") {
		t.Errorf("the refusal does not name the value that is missing:\n%s", out)
	}
}

// With generate off (the default), a name and nothing generates into it --
// the platform is assumed to have put something there already.
func TestByDefaultNothingGeneratesTheSecret(t *testing.T) {
	out, err := renderInfra(t, "--set", "postgres.runtimePasswordSecret=given-secret")
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	if strings.Contains(out, "kind: Password") || strings.Contains(out, "kind: ExternalSecret") {
		t.Error("a generator rendered even though generate was never set")
	}
	if !strings.Contains(out, "name: given-secret") {
		t.Error("the given secret name is not what the managed role points at")
	}
}

// Generating mints a Password and an ExternalSecret under the EXACT name
// given -- not a derived one -- so the managed role, the ExternalSecret's
// target and whatever the application chart was told all agree by
// construction rather than by two platforms spelling the same convention
// the same way. And the ExternalSecret is never refreshed.
//
// A refresh mints a NEW password. The operator updates the role to match
// it and every pod already holding the old one fails its next connection
// -- an outage with no deploy and no config change behind it. Rotation is
// a deliberate act with a restart beside it, not a timer.
func TestGeneratingMintsUnderTheExactNameGiven(t *testing.T) {
	out, err := renderInfra(t,
		"--set", "postgres.runtimePasswordSecret=us-devel-pg-runtime",
		"--set", "postgres.runtimePassword.generate=true",
	)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}
	for _, want := range []string{"kind: Password", "kind: ExternalSecret", "name: us-devel-pg-runtime"} {
		if !strings.Contains(out, want) {
			t.Errorf("generating did not render %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "name: example-pg-runtime") {
		t.Error("the generator used a name it derived instead of the one given")
	}
	if !strings.Contains(out, `refreshInterval: "0"`) {
		t.Error("the ExternalSecret refreshes, which mints a new password behind the role's back")
	}
}

// generate:true with no name is still a refusal, not a silently invented
// one -- generate says who writes it, and there is still nothing to write
// to without a name.
func TestGenerateWithNoNameStillRefuses(t *testing.T) {
	out, err := renderInfra(t, "--set", "postgres.runtimePassword.generate=true")
	if err == nil {
		t.Fatalf("the chart rendered with generate:true and no name:\n%s", out)
	}
	if !strings.Contains(out, "runtimePasswordSecret") {
		t.Errorf("the refusal does not name the missing value:\n%s", out)
	}
}

// In platform-owned mode the chart renders no Cluster, and the role the
// services log in as is a DatabaseRole against the platform's database.
func TestThePlatformOwnedInfraChartRendersNoCluster(t *testing.T) {
	out, err := renderInfra(t, infraDefaults("--set", "postgres.platformOwned=true")...)
	if err != nil {
		t.Fatalf("the chart does not render: %v\n%s", err, out)
	}

	if strings.Contains(out, "kind: Cluster") {
		t.Errorf("a Cluster was rendered although the platform owns the database:\n%s", out)
	}

	for _, want := range []string{"kind: DatabaseRole", "name: example-pg\n", "name: url_shortener_app", "name: example-pg-runtime"} {
		if !strings.Contains(out, want) {
			t.Errorf("the render lacks %q:\n%s", want, out)
		}
	}

	if strings.Contains(out, "kind: Database\n") {
		t.Errorf("a Database was rendered; the platform's chart declares the bootstrap database and a second object would be refused:\n%s", out)
	}
}

// The identity account is the one object two charts can render, and moving it
// must change nothing live: Argo CD applies both charts with one field
// manager, so an account the infra chart renders differently from the
// application chart's is an edit to the running object, not a move.
//
// This renders it from BOTH charts for the same release and compares what the object is: name, labels and annotations.
// The application chart renders it only on the test tier now, which is what the
// comparison uses. The
// annotations the infra copy has that the application's does not are the
// sync-wave and the guard, which are the point of moving it.
func TestTheInfraAccountIsTheOneTheApplicationRenders(t *testing.T) {
	app, err := render(t, defaults(
		"--set", "images.web.tag=dev",
		"--set", "serviceAccount.app.name=example-account",
		"--set", `serviceAccount.app.annotations.example\.io/role=archive`,
	)...)
	if err != nil {
		t.Fatalf("the application chart does not render: %v\n%s", err, app)
	}

	var fromApp map[string]any
	for _, m := range documents(t, app) {
		if m["kind"] == "ServiceAccount" && m["metadata"].(map[string]any)["name"] == "example-account" {
			fromApp = m
		}
	}
	if fromApp == nil {
		t.Fatalf("the application chart rendered no account named example-account:\n%s", app)
	}

	infra, err := renderInfra(t, infraDefaults(
		"--set", "tier=primary",
		"--set", "cloud.bucket=b", "--set", "cloud.iamName=i", "--set", "cloud.clusterName=c",
		"--set", "cloud.accountID=acct", "--set", "cloud.region=r",
		"--set", "cloud.serviceAccount=example-account",
		"--set", `cloud.serviceAccountAnnotations.example\.io/role=archive`,
	)...)
	if err != nil {
		t.Fatalf("the infra chart does not render: %v\n%s", err, infra)
	}
	fromInfra := docOfKind(t, infra, "ServiceAccount")

	am, im := fromApp["metadata"].(map[string]any), fromInfra["metadata"].(map[string]any)
	if am["name"] != im["name"] {
		t.Errorf("name: application %v, infra %v", am["name"], im["name"])
	}
	if !reflect.DeepEqual(am["labels"], im["labels"]) {
		t.Errorf("labels differ, so moving the account edits it:\napplication %v\ninfra       %v", am["labels"], im["labels"])
	}

	aa, ia := am["annotations"].(map[string]any), im["annotations"].(map[string]any)
	if ia["argocd.argoproj.io/sync-options"] != "Prune=false,Delete=false" {
		t.Errorf("the infra account lost the guard, and an apply from there would take it off the live object: %v", ia)
	}
	for k, v := range aa {
		if ia[k] != v {
			t.Errorf("annotation %s: application %v, infra %v", k, v, ia[k])
		}
	}
	for k := range ia {
		if _, ok := aa[k]; !ok && k != "argocd.argoproj.io/sync-wave" && k != "argocd.argoproj.io/sync-options" {
			t.Errorf("the infra account has an annotation the application's does not: %s", k)
		}
	}
	if ia["argocd.argoproj.io/sync-wave"] != "2" {
		t.Errorf("the account is not in wave 2, after the association: %v", ia)
	}
}

// Hard-wired, primary only, and ordered: the role, then the association, then
// the account. A test install renders none of the three.
func TestTheInfraAccountFollowsTheAssociationOnAPrimaryInstall(t *testing.T) {
	primary := []string{
		"--set", "tier=primary",
		"--set", "cloud.bucket=b", "--set", "cloud.iamName=i", "--set", "cloud.clusterName=c",
		"--set", "cloud.accountID=acct", "--set", "cloud.region=r", "--set", "cloud.serviceAccount=a",
	}

	out, err := renderInfra(t, infraDefaults(primary...)...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}

	annotation := func(out, kind, key string) any {
		a, _ := docOfKind(t, out, kind)["metadata"].(map[string]any)["annotations"].(map[string]any)
		return a[key]
	}
	const wave = "argocd.argoproj.io/sync-wave"
	if annotation(out, "Role", wave) != "0" || annotation(out, "PodIdentityAssociation", wave) != "1" || annotation(out, "ServiceAccount", wave) != "2" {
		t.Errorf("the role is wave %v, the association %v and the account %v, want 0, 1 and 2",
			annotation(out, "Role", wave), annotation(out, "PodIdentityAssociation", wave), annotation(out, "ServiceAccount", wave))
	}
	if annotation(out, "ServiceAccount", "argocd.argoproj.io/sync-options") != "Prune=false,Delete=false" {
		t.Error("the identity account does not carry the prune/delete guard")
	}
	if got := docOfKind(t, out, "ServiceAccount")["metadata"].(map[string]any)["name"]; got != "a" {
		t.Errorf("the account is named %v, want the one the association binds (a)", got)
	}

	// No key switches it, and a test install renders none of the cloud kinds.
	out, err = renderInfra(t, infraDefaults()...)
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}
	if strings.Contains(out, "kind: ServiceAccount") {
		t.Errorf("a test install rendered an account:\n%s", out)
	}
	if out, err := renderInfra(t, infraDefaults("--set", "cloud.serviceAccountManaged=true")...); err == nil {
		t.Errorf("cloud.serviceAccountManaged is accepted; the convention has no switch:\n%s", out)
	}
}
