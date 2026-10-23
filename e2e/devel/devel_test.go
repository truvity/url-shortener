package devel_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/truvity/url-shortener/e2e/devel"
)

func config() devel.Config {
	return devel.Config{
		Namespace: "ci-example", InstallsDomain: "installs.example.test", Issuer: "https://issuer.example.test",
		ArchiveBucket: "test-bucket", ArchiveRegion: "eu-central-1", Release: "us-r42-a1",
	}
}

func TestTheNamesDeriveFromTheRelease(t *testing.T) {
	c := config()

	for got, want := range map[string]string{
		c.InfraRelease():  "us-r42-a1-infra",
		c.Cluster():       "us-r42-a1-infra-pg",
		c.DatabaseHost():  "us-r42-a1-infra-pg-rw",
		c.OwnerSecret():   "us-r42-a1-infra-pg-app",
		c.RuntimeSecret(): "us-r42-a1-pg-runtime",
		c.CAConfigMap():   "us-r42-a1-pg-ca",
		c.Host():          "us-r42-a1.installs.example.test",
		c.BaseURL():       "https://us-r42-a1.installs.example.test",
		c.ArchivePrefix(): "ci-example/us-r42-a1/url-shortener/requests",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

// The public host is ONE label under the installs wildcard: a name with a dot
// in the release would be two labels, which the wildcard's certificate does
// not cover.
func TestTheHostIsOneLabel(t *testing.T) {
	c := config()

	label, _, _ := strings.Cut(c.Host(), ".")
	if label != c.Release || strings.Contains(c.Release, ".") {
		t.Errorf("host %q is not release.domain", c.Host())
	}
}

func TestEveryAppAccountButTheArchiversIsMadeByTheInstall(t *testing.T) {
	c := config()
	set := strings.Join(c.AppSet(), "\n")

	for _, want := range []string{
		"serviceAccount.create=false",
		"serviceAccount.components.log.name=" + devel.StandingS3Account,
		"route.hostname=" + c.Host(),
		"route.parentRef.kind=ListenerSet",
		"installName=" + c.Release,
		"archive.prefix=" + c.ArchivePrefix(),
	} {
		if !strings.Contains(set, want) {
			t.Errorf("the application values lack %q", want)
		}
	}

	// The standing account must never be one the install creates or deletes.
	for _, sa := range c.Accounts() {
		if sa == devel.StandingS3Account {
			t.Errorf("the install would create and delete the standing account %s", sa)
		}
	}

	if got := len(c.Accounts()); got != 5 {
		t.Errorf("%d accounts, want redirect, urls, web, stat and the migration's", got)
	}
}

func TestBothReleasesAreToldTheSameInstallName(t *testing.T) {
	c := config()
	want := "installName=" + c.Release

	for name, set := range map[string][]string{"infra": c.InfraSet(), "app": c.AppSet()} {
		found := false

		for _, s := range set {
			found = found || s == want
		}

		if !found {
			t.Errorf("the %s release is not given %s: the stream and its consumers would be named differently on the two sides", name, want)
		}
	}
}

func TestDefaultRelease(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	if got := devel.DefaultRelease(env(map[string]string{"GITHUB_RUN_ID": "39663134924", "GITHUB_RUN_ATTEMPT": "2"})); got != "us-r39663134924-a2" {
		t.Errorf("CI release %q", got)
	}

	if got := devel.DefaultRelease(env(map[string]string{"GITHUB_RUN_ID": "7"})); got != "us-r7-a1" {
		t.Errorf("CI release without an attempt %q", got)
	}

	got := devel.DefaultRelease(env(map[string]string{"USER": "Jane.Doe-Long-Name"}))
	if got != "us-l-janedoelongn" || devel.IsRunRelease(got) {
		t.Errorf("a person's release %q must be valid and never swept", got)
	}
}

func TestFromEnvNamesEveryMissingVariable(t *testing.T) {
	for _, k := range []string{"E2E_INSTALLS_DOMAIN", "SLUIS_ISSUER", "E2E_ARCHIVE_BUCKET", "E2E_ARCHIVE_REGION"} {
		t.Setenv(k, "")
	}

	_, err := devel.FromEnv()
	if err == nil {
		t.Fatal("an empty environment was accepted")
	}

	for _, k := range []string{"E2E_INSTALLS_DOMAIN", "SLUIS_ISSUER", "E2E_ARCHIVE_BUCKET", "E2E_ARCHIVE_REGION"} {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("the error does not name %s: %v", k, err)
		}
	}
}

func TestStaleReleases(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	old, fresh := now.Add(-5*time.Hour), now.Add(-10*time.Minute)

	got := devel.StaleReleases([]devel.Listed{
		{Name: "us-r1-a1", Updated: old},
		{Name: "us-r1-a1-infra", Updated: old},
		{Name: "us-r2-a1-infra", Updated: old}, // its application release is already gone
		{Name: "us-r3-a1", Updated: fresh},     // a run in progress
		{Name: "us-r4-a1", Updated: old},       // this install
		{Name: "us-l-jane", Updated: old},      // a person's, never swept
		{Name: "something-else", Updated: old}, // not ours
	}, "us-r4-a1", 3*time.Hour, now)

	want := []string{"us-r1-a1", "us-r2-a1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("swept %v, want %v", got, want)
	}
}

func TestChartTgzsSkipsTheInfraAndTheLegacyE2EChart(t *testing.T) {
	dir := t.TempDir()

	for _, f := range []string{"url-shortener-1.2.3.tgz", "url-shortener-infra-1.2.3.tgz", "url-shortener-e2e-1.2.3.tgz", "service-lib-1.2.3.tgz"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	app, infra, err := devel.ChartTgzs(dir)
	if err != nil {
		t.Fatal(err)
	}

	if filepath.Base(app) != "url-shortener-1.2.3.tgz" || filepath.Base(infra) != "url-shortener-infra-1.2.3.tgz" {
		t.Errorf("app %s infra %s", app, infra)
	}

	if _, _, err := devel.ChartTgzs(t.TempDir()); err == nil {
		t.Error("an empty directory was accepted")
	}
}

func TestATokenSourceMintsAgainBeforeTheTokenExpires(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	minted := 0

	jwt := func(exp time.Time) string {
		payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp.Unix())))

		return "h." + payload + ".s"
	}

	source := devel.NewTestTokenSource(func() string {
		minted++

		return jwt(now.Add(15 * time.Minute))
	}, func() time.Time { return now })

	for range 3 {
		if _, err := source.Token(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	if minted != 1 {
		t.Fatalf("minted %d times for three calls inside the token's life", minted)
	}

	now = now.Add(12 * time.Minute) // inside the four-minute refresh margin

	if _, err := source.Token(t.Context()); err != nil {
		t.Fatal(err)
	}

	if minted != 2 {
		t.Fatalf("minted %d times, want a fresh token once the old one is near its end", minted)
	}
}

func TestArchivedLine(t *testing.T) {
	prefix := "ci-example/us-r42-a1/"
	logs := strings.Join([]string{
		`{"event":"starting","component":"log"}`,
		`not json at all`,
		`{"event":"archived","key":"ci-example/other-release/o.ndjson","records":3}`,
		`{"event":"archived","key":"` + prefix + `url-shortener/requests/a.ndjson","records":0}`,
	}, "\n")

	if err := devel.ArchivedLine(logs, prefix); err == nil {
		t.Fatal("another release's object, and an empty one, were accepted")
	}

	logs += "\n" + `{"event":"archived","key":"` + prefix + `url-shortener/requests/b.ndjson","records":2}`
	if err := devel.ArchivedLine(logs, prefix); err != nil {
		t.Fatalf("a real archived line was refused: %v", err)
	}
}

func TestCountArchived(t *testing.T) {
	prefix := "ci-example/us-r42-a1/"
	logs := strings.Join([]string{
		`{"event":"archived","key":"` + prefix + `a","records":1}`,
		`{"event":"archived","key":"` + prefix + `b","records":0}`,
		`{"event":"archived","key":"ci-example/other/c","records":4}`,
		`{"msg":"archived","key":"` + prefix + `d","records":2}`,
		`junk`,
	}, "\n")

	if got := devel.CountArchived(logs, prefix); got != 2 {
		t.Errorf("counted %d, want 2 (an empty batch and another release's object do not count)", got)
	}
}
