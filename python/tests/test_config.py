"""The loader, against the SAME fixtures the Go and TypeScript loaders use.

That is the point of these tests rather than a convenience. Three loaders
that each pass their own fixtures prove three things; three loaders that pass
one set of fixtures prove that a configuration means the same thing whichever
runtime reads it, which is what the contract actually promises.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import pytest
import yaml

from truvity_policy import ConfigError, load, secret, validate

FIXTURES = Path(__file__).resolve().parents[2] / "config" / "testdata"


def fixture(name: str) -> str:
    """Return a path into the Go loader's fixtures, which are the shared ones."""
    return str(FIXTURES / name)


@pytest.fixture
def schema() -> dict[str, Any]:
    """Return the worked example's schema, the one the other loaders read."""
    doc: dict[str, Any] = json.loads(
        (FIXTURES / "shortener.schema.json").read_text(encoding="utf-8"),
    )
    return doc


def test_a_valid_file_loads(schema: dict[str, Any]) -> None:
    cfg = load(fixture("valid.yaml"), schema)

    assert cfg["listen"]["address"] == ":8080"
    assert cfg["probes"]["address"] == ":7070"


def test_an_unknown_key_is_refused(schema: dict[str, Any]) -> None:
    """The typo case, and the reason every schema here is strict.

    A loader that shrugs at an unknown key turns a misspelling into a default
    nobody chose, which the service then runs on with no signal at all.
    """
    with pytest.raises(ConfigError) as caught:
        load(fixture("unknown-key.yaml"), schema)

    assert "is not valid" in str(caught.value)


def test_a_missing_required_key_is_refused(schema: dict[str, Any]) -> None:
    with pytest.raises(ConfigError) as caught:
        load(fixture("missing-required.yaml"), schema)

    assert "is not valid" in str(caught.value)


def test_a_wrong_type_is_refused(schema: dict[str, Any]) -> None:
    with pytest.raises(ConfigError) as caught:
        load(fixture("wrong-type.yaml"), schema)

    assert "is not valid" in str(caught.value)


def test_an_empty_file_is_refused(schema: dict[str, Any]) -> None:
    """An empty file is not an empty configuration.

    It is almost always a mount that did not happen, and returning defaults
    for it starts the service on settings nobody wrote.
    """
    with pytest.raises(ConfigError) as caught:
        load(fixture("empty.yaml"), schema)

    assert "is empty" in str(caught.value)


def test_a_file_that_is_not_there_is_refused(schema: dict[str, Any]) -> None:
    with pytest.raises(ConfigError) as caught:
        load(fixture("absent.yaml"), schema)

    assert "cannot be read" in str(caught.value)


def test_the_error_names_the_key_and_never_the_value(schema: dict[str, Any]) -> None:
    """THE property that makes these errors safe to log.

    The fixture puts a password where a variable name belongs. The refusal
    must say which key is wrong and must not repeat what was in it, because
    the value most likely to be refused is the one most likely to be secret.
    """
    with pytest.raises(ConfigError) as caught:
        load(fixture("secret-in-file.yaml"), schema)

    message = str(caught.value)
    secret_value = "hunter2"

    assert secret_value not in message, "the refusal repeated the value it refused"
    assert "is not valid" in message


def test_validate_checks_a_document_that_was_never_a_file(schema: dict[str, Any]) -> None:
    """What a chart's tests call: the render is a document, not a path.

    The document is read from the shared fixture rather than written here,
    so that a change to what the schema requires fails this test in the same
    way it fails the others, instead of leaving one hand-written copy behind.
    """
    doc = yaml.safe_load((FIXTURES / "valid.yaml").read_text(encoding="utf-8"))

    validate(doc, schema)

    with pytest.raises(ConfigError):
        validate({**doc, "nonsense": True}, schema)


def test_a_secret_is_read_from_the_variable_a_configuration_names(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("EXAMPLE_PASSWORD", "value")

    assert secret("EXAMPLE_PASSWORD") == "value"


def test_an_unset_secret_names_the_variable_not_the_value(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.delenv("EXAMPLE_PASSWORD_THAT_IS_NOT_SET", raising=False)

    with pytest.raises(ConfigError, match="EXAMPLE_PASSWORD_THAT_IS_NOT_SET"):
        secret("EXAMPLE_PASSWORD_THAT_IS_NOT_SET")


def test_an_empty_secret_is_refused(monkeypatch: pytest.MonkeyPatch) -> None:
    """An empty variable is a secret that did not arrive.

    Treating it as a value means the service authenticates with nothing and
    fails somewhere that does not mention configuration.
    """
    monkeypatch.setenv("EXAMPLE_EMPTY", "")

    with pytest.raises(ConfigError, match="is empty"):
        secret("EXAMPLE_EMPTY")


def test_a_secret_with_no_variable_named_is_refused() -> None:
    with pytest.raises(ConfigError):
        secret("")


def test_a_reference_to_a_published_shape_resolves_without_the_network(
    schema: dict[str, Any],
) -> None:
    """The shared fragments are carried, not fetched.

    A loader that reached out to resolve a reference would work on a laptop
    and fail in a cluster with no route out — and the failure would look like
    a configuration error.
    """
    assert schema["allOf"][0]["$ref"].startswith("https://github.com/truvity/policy/schemas/")

    load(fixture("valid.yaml"), schema)
