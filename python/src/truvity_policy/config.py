"""Load a service's configuration: one file, one schema, secrets by name.

That is the whole of it, deliberately. No lifecycle, no dependency wiring, no
HTTP, no reflection over the environment — those are what a configuration
package grows into when nobody says it must not, and a service that depends
on them cannot be understood without them.

The contract this implements is ``docs/contracts/config.md``. The Go and
TypeScript loaders in this repository implement the same one, against the
same fixtures.
"""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any

import yaml
from jsonschema import Draft202012Validator
from jsonschema.exceptions import SchemaError
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

from .schemas import SHARED_SCHEMAS

__all__ = ["ConfigError", "load", "secret", "validate"]


class ConfigError(Exception):
    """What :func:`load`, :func:`validate` and :func:`secret` raise.

    It names the file and every failing path, and never contains a value from
    the file: a configuration sits next to the NAME of a secret, errors are
    logged, and a message that quoted what it refused would put it in every
    log that records the refusal.
    """

    def __init__(
        self,
        message: str,
        *,
        file: str | None = None,
        failures: list[str] | None = None,
    ) -> None:
        """Build the message a person reads, from the file and the failures."""
        self.file = file
        self.failures = failures or []

        detail = message
        if self.failures:
            joined = "\n  ".join(self.failures)
            detail = f"{message}:\n  {joined}"

        super().__init__(f"configuration {file} {detail}" if file else f"configuration {detail}")


def load(path: str | Path, schema: dict[str, Any]) -> Any:  # noqa: ANN401
    """Read the configuration file at *path*, validate it, and return it.

    The file is YAML, which means JSON is accepted too. Validation happens
    BEFORE anything is returned, so a caller never sees a value the schema
    would have rejected.

    Any reference to a shape this repository publishes resolves from the
    copies compiled into this package; nothing is fetched.
    """
    name = str(path)

    try:
        raw = Path(path).read_text(encoding="utf-8")
    except OSError as cause:
        raise ConfigError("cannot be read", file=name) from cause

    try:
        doc = yaml.safe_load(raw)
    except yaml.YAMLError as cause:
        raise ConfigError("is not valid YAML", file=name) from cause

    if doc is None:
        raise ConfigError("is empty", file=name)

    failures = _check(doc, schema)
    if failures:
        raise ConfigError("is not valid", file=name, failures=failures)

    return doc


def validate(doc: Any, schema: dict[str, Any]) -> None:  # noqa: ANN401
    """Check an already-parsed document against *schema*, raising on failure.

    Public because a chart's tests validate what they render with the same
    call, which is what stops the two drifting.
    """
    failures = _check(doc, schema)
    if failures:
        raise ConfigError("is not valid", failures=failures)


def secret(name: str) -> str:
    """Read the environment variable a configuration NAMES.

    A configuration file carries the name of the variable, never the value:
    files are rendered into config maps, printed when somebody debugs a
    deployment, and committed as test fixtures, and a secret has to survive
    all three being true.

    An unset or empty variable raises, and the error names the variable
    rather than quoting anything.
    """
    if not name:
        raise ConfigError("no environment variable was named for this secret")

    value = os.environ.get(name)
    if value is None:
        raise ConfigError(f"environment variable {name} is not set")

    if value == "":
        raise ConfigError(f"environment variable {name} is empty")

    return value


def _registry() -> Registry:
    """Every shape this repository publishes, by the identifier it carries.

    Built once per call rather than cached globally: the cost is a dictionary
    walk, and a cache keyed on a mutable schema is a bug waiting for the
    first caller who edits one.
    """
    return Registry().with_resources(
        (identifier, Resource.from_contents(doc, default_specification=DRAFT202012))
        for identifier, doc in SHARED_SCHEMAS.items()
    )


def _check(doc: Any, schema: dict[str, Any]) -> list[str]:  # noqa: ANN401
    """Return every way *doc* fails *schema*, sorted, or an empty list.

    Every failure, not the first: a person fixing a configuration wants all
    the failing keys at once, not one per run.

    ``format`` is left as an annotation, which is a decision rather than an
    omission. In draft 2020-12 it does not assert unless a validator is told
    otherwise, and the other two loaders leave it alone. A ``format`` that
    bit in one runtime and not the others would mean a configuration this
    repository accepts in a chart's tests and refuses in the service, which
    is the exact drift these loaders exist to prevent. Where a rule must
    bite, the schema says ``pattern``.
    """
    try:
        validator = Draft202012Validator(schema, registry=_registry())
    except SchemaError as cause:
        raise ConfigError("cannot be checked: the schema itself is not valid") from cause

    failures = {_describe(error) for error in validator.iter_errors(doc)}

    return sorted(failures)


def _describe(error: Any) -> str:  # noqa: ANN401
    """Name the failing key and what is wrong with it, and nothing else.

    Never the value. A message that quotes what it refused puts the refused
    value into every log that records the refusal, and the one kind of value
    most likely to be refused is the one most likely to be a secret.
    """
    where = "/".join(str(part) for part in error.absolute_path)

    if error.validator == "required":
        missing = error.message.split("'")[1] if "'" in error.message else "a key"
        return f"{where}/{missing}: is required" if where else f"{missing}: is required"

    if error.validator in {"additionalProperties", "unevaluatedProperties"}:
        return f"{where or '(root)'}: {error.message}"

    return f"{where or '(root)'}: {error.message}"
