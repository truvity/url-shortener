// Command stat counts redirects.
//
// It consumes what the redirect service published and increments a counter,
// which is the whole of it. The interesting part is the consumer: durable by
// name, so that every replica is one consumer group and a restart resumes
// where it left off rather than replaying or losing what arrived while it was
// gone.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"golang.org/x/sync/errgroup"

	"github.com/truvity/policy/transport"

	"github.com/truvity/policy/examples/url-shortener/internal/business/stat"
	"github.com/truvity/policy/examples/url-shortener/internal/config"
	"github.com/truvity/policy/examples/url-shortener/internal/runtime"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func run() error {
	path := flag.String("config", os.Getenv("CONFIG_FILE"), "path to the configuration file")
	flag.Parse()
	if *path == "" {
		return errors.New("no configuration file: pass -config or set CONFIG_FILE")
	}

	cfg, err := config.LoadStat(*path)
	if err != nil {
		return err
	}

	log := runtime.Logger(cfg.Log.Level)
	version, commit := runtime.Version()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.InfoContext(ctx, "starting", slog.String("component", "stat"),
		slog.String("version", version), slog.String("commit", commit))

	nc, err := connect(cfg.Events.NATS)
	if err != nil {
		return err
	}
	defer func() { _ = nc.Drain() }()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("open jetstream: %w", err)
	}

	// The identity this process was mounted, if the platform provides one.
	// The counter serves nothing, so this is used only as a CLIENT — it
	// presents the certificate and checks who answered.
	identity, err := transport.Load(cfg.TLS, log)
	if err != nil {
		return fmt.Errorf("transport identity: %w", err)
	}

	// The counter holds no database credential. It asks the service that
	// owns the table, which is the whole of the ownership rule: a component
	// that cannot write the table cannot write it wrongly.
	counter := stat.NewRemote(runtime.RPCClient(identity, 10*time.Second), cfg.Urls.Address)
	handler := stat.NewHandler(ctx, log, stat.NewManager(ctx, log, counter))

	// The stream exists already — a component does not create the stream it
	// reads, because two components disagreeing about a stream's retention is
	// a data-loss argument nobody wins at run time.
	stream, err := js.Stream(ctx, cfg.Events.Consumer.Stream)
	if err != nil {
		return fmt.Errorf("find stream %s: %w", cfg.Events.Consumer.Stream, err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       cfg.Events.Consumer.Durable,
		FilterSubject: cfg.Events.Consumer.Subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		// Redelivery is what makes a failed increment a retry rather than a
		// lost click. The handler terms a message it cannot parse, so a
		// poison message does not retry forever.
		MaxDeliver: 5,
		AckWait:    30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("bind consumer %s: %w", cfg.Events.Consumer.Durable, err)
	}

	consuming, err := consumer.Consume(func(msg jetstream.Msg) {
		if err := handler.HandleNATSMessage(ctx, msg); err != nil {
			log.ErrorContext(ctx, "message not handled", slog.Any("error", err))
			// No Ack: let it be redelivered. A click counted zero times is
			// worse than one counted twice, and the increment is by URL.
			return
		}
		if err := msg.Ack(); err != nil {
			log.ErrorContext(ctx, "acknowledge failed", slog.Any("error", err))
		}
	})
	if err != nil {
		return fmt.Errorf("start consuming: %w", err)
	}
	defer consuming.Stop()

	probes := runtime.Probes(cfg.Probes.Address, func(_ context.Context) error {
		// The stream, and nothing else. Readiness reports whether this
		// component can do its work, and it cannot consume without the
		// broker — but a URL service that is down is not this component's
		// outage to report. A probe that checked it would take the counter
		// out of rotation for somebody else's problem, and the messages
		// would pile up in the stream either way.
		if !nc.IsConnected() {
			return errors.New("not connected to the event stream")
		}
		return nil
	})

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return runtime.Serve(groupCtx, log, "probes", probes, 5*time.Second) })
	group.Go(func() error {
		<-groupCtx.Done()
		log.InfoContext(groupCtx, "draining", slog.String("consumer", cfg.Events.Consumer.Durable))
		// Drain finishes the messages already pulled; Stop would abandon
		// them and they would be redelivered to somebody else.
		consuming.Drain()
		return nil
	})
	return group.Wait()
}

func connect(cfg config.NATS) (*nats.Conn, error) {
	opts, err := runtime.NATSOptions("url-shortener-stat", cfg.TokenFile)
	if err != nil {
		return nil, err
	}

	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to the event stream: %w", err)
	}
	return nc, nil
}
