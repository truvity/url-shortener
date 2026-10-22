// Command e2ectl is the integration suite's install half, as explicit steps a
// workflow runs one by one (so the uninstall is its own `if: always()` step,
// which holds even when the job is cancelled, where a shell trap does not):
//
//	e2ectl sweep       remove the pairs of earlier runs that were killed
//	e2ectl install     install the release into the CI namespace
//	e2ectl wait        wait until the release's public host answers
//	e2ectl env         print the variables the test process reads
//	e2ectl uninstall   remove the release
//
// It reaches the cluster through the Kubernetes API only (see package devel),
// and takes everything about the estate from the environment.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/truvity/url-shortener/e2e/devel"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "e2ectl:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: e2ectl sweep|install|wait|env|uninstall")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := devel.FromEnv()
	if err != nil {
		return err
	}

	switch os.Args[1] {
	case "sweep":
		return devel.Sweep(ctx, cfg, 3*time.Hour)
	case "install":
		return devel.Install(ctx, cfg)
	case "wait":
		return devel.WaitPublic(ctx, cfg.BaseURL(), devel.NewTokenSource(cfg.Issuer))
	case "uninstall":
		// Not the signal-cancelled context: a cancelled job must still
		// clean up.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Minute)
		defer cancel()

		return devel.Uninstall(cleanup, cfg)
	case "env":
		env := devel.Env(cfg)

		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}

		sort.Strings(keys)

		for _, k := range keys {
			fmt.Printf("%s=%s\n", k, env[k])
		}

		return nil
	default:
		return fmt.Errorf("unknown verb %q", os.Args[1])
	}
}
