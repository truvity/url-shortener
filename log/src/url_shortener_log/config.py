"""What this binary reads, and the shape it reads it into.

Several field names here are camelCase, which is not how Python is written.
They are not Python names: they are the KEYS of a JSON Schema, and a
`TypedDict` whose fields disagree with the document it describes is a
description of a different document.

One file, one schema, validated before anything is constructed — the same
rule the Go components follow, using the same loader. The types here are
`TypedDict`s rather than a second validation library: the schema in
``schemas/log.json`` is the description of this file, and a second
description of the same shape is a thing that drifts.
"""

from __future__ import annotations

import json
from importlib.resources import files
from typing import TYPE_CHECKING, NotRequired, TypedDict

from truvity_policy import load

if TYPE_CHECKING:
    from typing import Any

# Carried INSIDE the package, so the binary validates against the schema it
# shipped with rather than one a deployment happened to mount beside it —
# the same property the Go components get from `go:embed`. See README.md in
# this directory for why it is not in ../../schemas/ with the others.
SCHEMA = files(__package__ or "url_shortener_log") / "log.schema.json"


class Probes(TypedDict):
    """Where the liveness and readiness listener binds."""

    address: str


class Log(TypedDict):
    """The one level everything is emitted at."""

    level: str


class Drain(TypedDict):
    """How long in-flight work is given when the process is asked to stop."""

    seconds: int


class NATS(TypedDict):
    """Where the broker is, and the file holding the token to reach it."""

    url: str
    tokenFile: NotRequired[str]


class Consumer(TypedDict):
    """The stream, the durable name shared by every replica, and the filter."""

    stream: str
    durable: str
    subject: NotRequired[str]


class Events(TypedDict):
    """The broker and what is consumed from it."""

    nats: NATS
    consumer: Consumer


class CredentialsEnv(TypedDict):
    """The NAMES of the variables holding the credentials, never the values."""

    accessKeyID: str
    secretAccessKey: str


class Bucket(TypedDict):
    """An object store addressed by the S3 API. The vendor is not in here."""

    name: str
    region: NotRequired[str]
    endpoint: NotRequired[str]
    ca: NotRequired[str]
    pathStyle: NotRequired[bool]
    credentialsEnv: NotRequired[CredentialsEnv]


class Batch(TypedDict):
    """When an object is closed and written. Both limits apply."""

    maxRecords: NotRequired[int]
    maxSeconds: NotRequired[int]


class Archive(TypedDict):
    """Where the records go and how they are grouped."""

    bucket: Bucket
    prefix: NotRequired[str]
    batch: NotRequired[Batch]


class Config(TypedDict):
    """The whole file."""

    probes: Probes
    log: NotRequired[Log]
    drain: NotRequired[Drain]
    events: Events
    archive: Archive


def schema() -> dict[str, Any]:
    """Read this binary's schema — the one the chart's tests validate against."""
    doc: dict[str, Any] = json.loads(SCHEMA.read_text(encoding="utf-8"))
    return doc


def read(path: str) -> Config:
    """Load and validate the configuration file, or raise naming what is wrong."""
    cfg: Config = load(path, schema())
    return cfg
