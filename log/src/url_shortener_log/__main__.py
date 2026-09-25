"""The composition root.

Read this file top to bottom and you know what the process is made of and
what it talks to. There is no container to ask and no registration in a
package far away — the same rule the Go components follow, and the reason
they are readable.
"""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import os
import signal
import sys
from pathlib import Path
from typing import TYPE_CHECKING

import nats
from nats.errors import TimeoutError as NATSTimeoutError
from nats.js.api import AckPolicy, ConsumerConfig
from opentelemetry import trace
from opentelemetry.trace import SpanKind, StatusCode
from truvity_policy import ConfigError, telemetry

from . import archive, config, runtime

if TYPE_CHECKING:
    from nats.aio.msg import Msg

COMPONENT = "log"

# How long a fetch waits before looping. Short, because the loop is also
# what notices the deadline on a half-full batch and what notices that it has
# been asked to stop.
FETCH_SECONDS = 1

# Redelivery is what makes a failed write a retry rather than a lost record.
ACK_WAIT_SECONDS = 60
MAX_DELIVER = 5

# What a drain is given when the configuration does not say. The chart always
# says; this is what a hand-written file falls back to.
DEFAULT_DRAIN_SECONDS = 20


def main() -> int:
    """Run, and turn any refusal into one line on stderr and a non-zero exit."""
    parser = argparse.ArgumentParser(prog="url-shortener-log")
    parser.add_argument(
        "-config",
        "--config",
        dest="config",
        default=os.environ.get("CONFIG_FILE", ""),
        help="path to the configuration file",
    )
    args = parser.parse_args()
    if not args.config:
        print(  # noqa: T201 — a refusal before the logger exists has nowhere else to go
            "no configuration file: pass -config or set CONFIG_FILE",
            file=sys.stderr,
        )
        return 1

    try:
        cfg = config.read(args.config)
    except ConfigError as error:
        print(str(error), file=sys.stderr)  # noqa: T201 — as above
        return 1

    # Telemetry, from OpenTelemetry's own environment (decision 0006).
    # With no endpoint configured the chart sets the exporters to `none`
    # and this installs nothing, so a laptop and a cluster run the same
    # code down the same path.
    shutdown_telemetry = telemetry.start()
    try:
        return asyncio.run(run(cfg))
    finally:
        # Flush before the process goes. An archiver that dies with a
        # buffer full of spans describing why is the one case the spans
        # were for.
        shutdown_telemetry()


async def run(cfg: config.Config) -> int:  # noqa: C901, PLR0915 — a composition root IS the wiring
    """Wire the process up, serve until asked to stop, then drain."""
    log = runtime.logger(cfg.get("log", {}).get("level", "info"))
    version, commit = runtime.version()
    log.info("starting", component=COMPONENT, version=version, commit=commit)

    events = cfg["events"]
    settings = cfg["archive"]
    batch = settings.get("batch", {})

    # The store. Reached once before serving, so that a bucket nobody granted
    # access to is a refusal at start-up rather than a surprise an hour later
    # when the first batch fills.
    bucket = settings["bucket"]
    s3 = archive.client(bucket)
    await asyncio.to_thread(s3.head_bucket, Bucket=bucket["name"])
    writer = archive.Writer(
        s3,
        bucket["name"],
        prefix=settings.get("prefix", archive.DEFAULT_PREFIX),
        max_records=batch.get("maxRecords", archive.DEFAULT_MAX_RECORDS),
        max_seconds=batch.get("maxSeconds", archive.DEFAULT_MAX_SECONDS),
    )

    # The broker. The token is a FILE the platform mounts, never a value in
    # the configuration — and it is read on every CONNECT rather than once
    # here.
    #
    # That is the whole reason this is a callable. The token is short-lived
    # and the runtime replaces the file in place, so a client that reads it
    # once authenticates fine until its first reconnect and then fails with
    # an authorisation error naming nothing that changed. Measured on a
    # sibling service: the broker closed the connection sixty minutes in,
    # the reconnect presented the same expired token, and the consumer was
    # gone until somebody restarted the pod.
    token = None
    if token_file := events["nats"].get("tokenFile"):
        token_path = Path(token_file)

        def read_token() -> str:
            return token_path.read_text(encoding="utf-8").strip()

        # Read it once HERE so an unreadable file fails at start-up, with
        # the path, rather than at the first connect as an error about
        # authorisation.
        await asyncio.to_thread(read_token)
        token = read_token
    connection = await nats.connect(
        servers=[events["nats"]["url"]],
        name=f"url-shortener-{COMPONENT}",
        token=token,
    )

    # The stream exists already — a component does not create the stream it
    # reads, because two components disagreeing about a stream's retention is
    # a data-loss argument nobody wins at run time.
    consumer = events["consumer"]
    subscription = await connection.jetstream().pull_subscribe(
        subject=consumer.get("subject", ""),
        durable=consumer["durable"],
        stream=consumer["stream"],
        config=ConsumerConfig(
            ack_policy=AckPolicy.EXPLICIT,
            ack_wait=ACK_WAIT_SECONDS,
            max_deliver=MAX_DELIVER,
        ),
    )

    stopping = asyncio.Event()

    def ready() -> str | None:
        if stopping.is_set():
            return "draining"
        if not connection.is_connected:
            return "not connected to the event stream"
        return None

    probes = runtime.Probes(cfg["probes"]["address"], ready)
    probes.start()
    log.info("serving probes", address=cfg["probes"]["address"])

    loop = asyncio.get_running_loop()
    for received in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(received, stopping.set)

    held: list[Msg] = []

    tracer = trace.get_tracer("url-shortener-log")

    async def write() -> None:
        """Write what is held, then acknowledge it. In that order, always."""
        if not held:
            return
        # One span per FLUSH, not per message. A span per message would be
        # one per event on a stream this process reads continuously, which
        # is the cardinality problem batching exists to avoid in the first
        # place -- the unit of work here is the write, not the record.
        with tracer.start_as_current_span(
            "archive.flush", kind=SpanKind.PRODUCER, attributes={"archive.records": len(held)}
        ) as span:
            try:
                key = await asyncio.to_thread(writer.flush)
            except Exception as error:
                span.set_status(StatusCode.ERROR, str(error))
                span.record_exception(error)
                raise
            if key is not None:
                span.set_attribute("archive.key", key)
            for message in held:
                await message.ack()
            log.info("archived", key=key, records=len(held))
            held.clear()

    try:
        while not stopping.is_set():
            try:
                messages = await subscription.fetch(
                    batch=writer.capacity,
                    timeout=FETCH_SECONDS,
                )
            except NATSTimeoutError:
                messages = []

            for message in messages:
                writer.add(
                    archive.Record(
                        sequence=message.metadata.sequence.stream,
                        received=message.metadata.timestamp or archive.now(),
                        subject=message.subject,
                        headers=dict(message.headers or {}),
                        body=message.data,
                    ),
                )
                held.append(message)

            if writer.due(archive.now()):
                await write()

        # Draining. What was already pulled is finished rather than abandoned
        # to redelivery: the records are in this process's memory and nowhere
        # else, and the next replica would wait out the acknowledgement
        # deadline before seeing them.
        #
        # Bounded by the SAME number the chart gives the platform as a grace
        # period. A drain with no deadline of its own is one the platform
        # ends with a kill, half-written, and the two ways of disagreeing
        # about that number both look like a network fault.
        seconds = cfg.get("drain", {}).get("seconds", DEFAULT_DRAIN_SECONDS)
        log.info("draining", held=len(held), seconds=seconds)
        try:
            await asyncio.wait_for(write(), timeout=seconds)
        except TimeoutError:
            # Nothing was acknowledged, so nothing is lost: the whole batch
            # is redelivered to whoever consumes next.
            # Not `exception`: a deadline is a decision this process made, and a
            # traceback through asyncio.wait_for would say nothing about it.
            log.error(  # noqa: TRY400
                "drain did not finish in time", held=len(held), seconds=seconds
            )
    finally:
        probes.stop()
        with contextlib.suppress(Exception):
            await connection.drain()
        log.info("stopped", component=COMPONENT)

    return 0


if __name__ == "__main__":
    sys.exit(main())
