package devel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Timeouts. helm --wait returns when the pods are ready, which is earlier
// than the gateway serves the new route; WaitPublic covers that.
const (
	helmTimeout    = "10m"
	clusterTimeout = "10m"
)

// kubectl and helm are the Kubernetes API and nothing else: no port-forward,
// no exec.
func kubectl(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "kubectl", append([]string{"--context", Kubecontext}, args...)...) //nolint:gosec // fixed argv
}

func helm(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "helm", append([]string{"--kube-context", Kubecontext}, args...)...) //nolint:gosec // fixed argv
}

func output(cmd *exec.Cmd) (string, error) {
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()

	return strings.TrimSpace(buf.String()), err
}

func step(format string, args ...any) { fmt.Fprintf(os.Stderr, "==> "+format+"\n", args...) }

// ChartTgzs finds the packaged application and infrastructure charts.
func ChartTgzs(dir string) (app, infra string, err error) {
	all, err := filepath.Glob(filepath.Join(dir, App+"-*.tgz"))
	if err != nil {
		return "", "", err
	}

	sort.Strings(all)

	for _, f := range all {
		name := filepath.Base(f)

		switch {
		case strings.HasPrefix(name, App+"-infra-"):
			infra = f
		case strings.HasPrefix(name, App+"-e2e-"):
		default:
			app = f
		}
	}

	if app == "" || infra == "" {
		return "", "", fmt.Errorf("no packaged %s and %s-infra charts under %s: run `just snapshot` first", App, App, dir)
	}

	return app, infra, nil
}

// Install deploys the pair into the CI namespace through the Kubernetes API:
//
//  1. the accounts and the runtime role's credential, which the charts take
//     by name and never invent;
//  2. the infrastructure release (the database, the stream), and a wait for
//     the database to be ready;
//  3. the database authority, published as the ConfigMap the services verify
//     the server against;
//  4. the application release, whose pre-install hook migrates the schema in
//     the cluster (helm --wait returns only when it completed).
//
// Nothing here needs the database, the broker or the bucket from outside.
func Install(ctx context.Context, c Config) error {
	appTgz, infraTgz, err := ChartTgzs(c.ChartsDir)
	if err != nil {
		return err
	}

	step("accounts and the runtime role's credential in %s", c.Namespace)

	if err := c.ensureAccountsAndSecret(ctx); err != nil {
		return err
	}

	step("infrastructure release %s", c.InfraRelease())

	if err := helmInstall(ctx, c, c.InfraRelease(), infraTgz, c.InfraSet()); err != nil {
		Diagnose(ctx, c)

		return err
	}

	step("waiting for the database %s", c.Cluster())

	if out, err := output(kubectl(ctx, "-n", c.Namespace, "wait", "--for=condition=Ready",
		"cluster.postgresql.cnpg.io/"+c.Cluster(), "--timeout="+clusterTimeout)); err != nil {
		Diagnose(ctx, c)

		return fmt.Errorf("the database did not become ready: %w: %s", err, out)
	}

	step("publishing the database authority as %s", c.CAConfigMap())

	if err := c.publishCA(ctx); err != nil {
		return err
	}

	step("application release %s", c.Release)

	if err := helmInstall(ctx, c, c.Release, appTgz, c.AppSet()); err != nil {
		Diagnose(ctx, c)

		return err
	}

	return nil
}

func helmInstall(ctx context.Context, c Config, release, tgz string, set []string) error {
	args := []string{"upgrade", "--install", release, tgz, "--namespace", c.Namespace, "--wait", "--timeout", helmTimeout}
	for _, s := range set {
		args = append(args, "--set", s)
	}

	cmd := helm(ctx, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helm install %s: %w", release, err)
	}

	return nil
}

func (c Config) ensureAccountsAndSecret(ctx context.Context) error {
	for _, sa := range c.Accounts() {
		if out, err := output(kubectl(ctx, "-n", c.Namespace, "create", "serviceaccount", sa)); err != nil && !strings.Contains(out, "AlreadyExists") {
			return fmt.Errorf("create ServiceAccount %s: %w: %s", sa, err, out)
		}
	}

	password, err := randomPassword()
	if err != nil {
		return err
	}

	// basic-auth, as CNPG's managed role requires. Created once: a rerun of
	// the same release keeps the password the role already has.
	cmd := kubectl(ctx, "-n", c.Namespace, "create", "secret", "generic", c.RuntimeSecret(),
		"--type=kubernetes.io/basic-auth", "--from-literal=username=url_shortener_app", "--from-literal=password="+password)
	if out, err := output(cmd); err != nil && !strings.Contains(out, "AlreadyExists") {
		return fmt.Errorf("create Secret %s: %w", c.RuntimeSecret(), err)
	}

	return nil
}

// publishCA copies the Cluster's own authority (the Secret CNPG keeps, key
// ca.crt) into the ConfigMap the services' `verify-full` connection names. The
// certificate CNPG issues for <cluster>-rw carries the Service's namespace
// and cluster-domain names, which is what the chart dials.
func (c Config) publishCA(ctx context.Context) error {
	out, err := output(kubectl(ctx, "-n", c.Namespace, "get", "secret", c.Cluster()+"-ca", "-o", `jsonpath={.data.ca\.crt}`))
	if err != nil {
		return fmt.Errorf("read the database authority %s-ca: %w: %s", c.Cluster(), err, out)
	}

	pem, err := base64.StdEncoding.DecodeString(out)
	if err != nil || len(pem) == 0 {
		return fmt.Errorf("the database authority %s-ca holds no ca.crt", c.Cluster())
	}

	file, err := os.CreateTemp("", "pg-ca-*.crt")
	if err != nil {
		return err
	}

	defer func() { _ = os.Remove(file.Name()) }()

	if _, err := file.Write(pem); err != nil {
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	// create + apply, so a rerun replaces it.
	create := kubectl(ctx, "-n", c.Namespace, "create", "configmap", c.CAConfigMap(),
		"--from-file=ca-certificates.crt="+file.Name(), "--dry-run=client", "-o", "yaml")

	manifest, err := create.Output()
	if err != nil {
		return fmt.Errorf("render the CA ConfigMap: %w", err)
	}

	apply := kubectl(ctx, "-n", c.Namespace, "apply", "-f", "-")
	apply.Stdin = bytes.NewReader(manifest)

	if out, err := output(apply); err != nil {
		return fmt.Errorf("apply the CA ConfigMap: %w: %s", err, out)
	}

	return nil
}

// Uninstall removes what Install made, application release first. It is
// meant for `if: always()`: it tolerates everything already being gone.
func Uninstall(ctx context.Context, c Config) error {
	var errs []error

	for _, release := range []string{c.Release, c.InfraRelease()} {
		step("uninstalling %s", release)

		out, err := output(helm(ctx, "uninstall", release, "--namespace", c.Namespace, "--wait", "--timeout", helmTimeout, "--ignore-not-found"))
		if err != nil {
			errs = append(errs, fmt.Errorf("helm uninstall %s: %w: %s", release, err, out))
		}
	}

	args := []string{"-n", c.Namespace, "delete", "--ignore-not-found", "--wait=false",
		"secret/" + c.RuntimeSecret(), "configmap/" + c.CAConfigMap()}
	for _, sa := range c.Accounts() {
		args = append(args, "serviceaccount/"+sa)
	}

	if out, err := output(kubectl(ctx, args...)); err != nil {
		errs = append(errs, fmt.Errorf("delete the install's objects: %w: %s", err, out))
	}

	return errors.Join(errs...)
}

// Sweep uninstalls the pairs of earlier runs that were killed before their
// own cleanup (a cancelled job is SIGKILLed past every trap, and `always()`
// does not survive a runner that vanished). Only releases made for a run
// (IsRunRelease) older than maxAge are touched, and never this install's.
func Sweep(ctx context.Context, c Config, maxAge time.Duration) error {
	out, err := helm(ctx, "list", "--namespace", c.Namespace, "--output", "json").Output()
	if err != nil {
		return fmt.Errorf("list releases: %w", err)
	}

	var releases []struct {
		Name    string `json:"name"`
		Updated string `json:"updated"`
	}

	if err := json.Unmarshal(out, &releases); err != nil {
		return fmt.Errorf("parse the release list: %w", err)
	}

	var errs []error

	for _, name := range StaleReleases(releases2(releases), c.Release, maxAge, time.Now()) {
		step("sweeping %s", name)

		other := c
		other.Release = name

		if err := Uninstall(ctx, other); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// Listed is one row of `helm list`.
type Listed struct {
	Name    string
	Updated time.Time
}

func releases2(in []struct {
	Name    string `json:"name"`
	Updated string `json:"updated"`
}) []Listed {
	out := make([]Listed, 0, len(in))

	for _, r := range in {
		// helm prints "2026-10-22 21:00:00.123456 +0000 UTC".
		trimmed := r.Updated
		if i := strings.Index(trimmed, " m="); i >= 0 {
			trimmed = trimmed[:i]
		}

		t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", trimmed)
		if err != nil {
			continue
		}

		out = append(out, Listed{Name: r.Name, Updated: t})
	}

	return out
}

// StaleReleases picks the application releases to sweep: made for a run,
// not this install's, last updated before now-maxAge. The infra release
// (-infra) of a pair is removed with its application release, and one whose
// application release is gone is picked by its own name.
func StaleReleases(listed []Listed, current string, maxAge time.Duration, now time.Time) []string {
	seen := map[string]bool{}

	var out []string

	for _, r := range listed {
		name := strings.TrimSuffix(r.Name, "-infra")
		if !IsRunRelease(name) || name == current || seen[name] {
			continue
		}

		if now.Sub(r.Updated) < maxAge {
			continue
		}

		seen[name] = true
		out = append(out, name)
	}

	sort.Strings(out)

	return out
}

// Diagnose prints what the Kubernetes API knows about a release that did not
// come up. Best effort and read only: it runs on the way out of a failure
// that matters more.
func Diagnose(ctx context.Context, c Config) {
	section := func(title string, args ...string) {
		fmt.Fprintf(os.Stderr, "\n==== %s ====\n", title)

		cmd := kubectl(ctx, append([]string{"-n", c.Namespace}, args...)...)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "(%v)\n", err)
		}
	}

	sel := "app.kubernetes.io/instance in (" + c.Release + "," + c.InfraRelease() + ")"

	section("pods", "get", "pods", "-l", sel, "-o", "wide")
	section("jobs", "get", "jobs", "-l", sel)
	section("database", "get", "cluster.postgresql.cnpg.io/"+c.Cluster())
	section("stream", "get", "streams.jetstream.nats.io", "-l", sel)
	section("route", "get", "httproute", "-l", sel)
	section("events", "get", "events", "--sort-by=.lastTimestamp")
	section("migrate logs", "logs", "-l", "app.kubernetes.io/instance="+c.Release+",app.kubernetes.io/component=migrate", "--all-containers", "--tail=200", "--prefix")
}

// Env is what the test process reads: the release's public base URL, and
// the names the Kubernetes-door tests need.
func Env(c Config) map[string]string {
	return map[string]string{
		"E2E_URL":       c.BaseURL(),
		"E2E_NAMESPACE": c.Namespace,
		"E2E_RELEASE":   c.Release,
	}
}
