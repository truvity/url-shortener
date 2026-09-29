package main

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/truvity/policy/examples/url-shortener/internal/gen/urlshortener/v1"
	"github.com/truvity/policy/examples/url-shortener/internal/gen/urlshortener/v1/urlshortenerv1connect"
)

// countingUrls is a urls service whose click count for any key follows a
// script: script[i] is what the i-th read answers, and the last entry is
// held from then on. It stands for the urls service plus a consumer
// counting (or miscounting) the clicks.
type countingUrls struct {
	urlshortenerv1connect.UrlsServiceClient

	script []int64
	reads  atomic.Int64
}

func (c *countingUrls) Get(context.Context, *connect.Request[v1.GetRequest]) (*connect.Response[v1.GetResponse], error) {
	i := int(c.reads.Add(1)) - 1
	if i >= len(c.script) {
		i = len(c.script) - 1
	}
	return connect.NewResponse(&v1.GetResponse{Url: &v1.Url{ClickCount: c.script[i]}}), nil
}

func proberOver(script ...int64) *prober {
	return &prober{
		urls:           &countingUrls{script: script},
		statPatience:   200 * time.Millisecond,
		statPollPeriod: 5 * time.Millisecond,
		statSettle:     100 * time.Millisecond,
	}
}

func TestStatPassesWhenTheCountGoesUpByExactlyOne(t *testing.T) {
	// 0 while the consumer works, then 1 and it STAYS 1 through the hold.
	if _, err := proberOver(0, 0, 1).waitForOneClick(context.Background(), "k"); err != nil {
		t.Fatalf("a click counted once failed the journey: %v", err)
	}
}

func TestStatFailsWhenTheCountNeverMoves(t *testing.T) {
	_, err := proberOver(0).waitForOneClick(context.Background(), "k")
	if err == nil || !strings.Contains(err.Error(), "want exactly 1") {
		t.Fatalf("a counter that never moved passed the journey, err = %v", err)
	}
}

func TestStatFailsWhenTheClickIsCountedAgainAfterTheFirstRead(t *testing.T) {
	// The redelivery case: 1 at the first look, then 2 (and later 5).
	for _, script := range [][]int64{{0, 1, 1, 2}, {0, 1, 5}} {
		_, err := proberOver(script...).waitForOneClick(context.Background(), "k")
		if err == nil || !strings.Contains(err.Error(), "more than once") {
			t.Errorf("script %v: a click counted more than once passed the journey, err = %v", script, err)
		}
	}
}

func TestStatFailsAtOnceWhenTheCountOvershootsBeforeItEverReadsOne(t *testing.T) {
	_, err := proberOver(0, 2).waitForOneClick(context.Background(), "k")
	if err == nil || !strings.Contains(err.Error(), "click_count is 2") {
		t.Fatalf("an overshoot passed the journey, err = %v", err)
	}
}

func TestStatHoldCanBeTurnedOff(t *testing.T) {
	p := proberOver(1, 2)
	p.statSettle = 0
	if _, err := p.waitForOneClick(context.Background(), "k"); err != nil {
		t.Fatalf("statSettle 0 still held: %v", err)
	}
}
