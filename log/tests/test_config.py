"""What this binary accepts as a configuration file, and what it refuses."""

from __future__ import annotations

from typing import TYPE_CHECKING, Any

import pytest
import yaml
from truvity_policy import ConfigError, validate

from url_shortener_log import config

if TYPE_CHECKING:
    from pathlib import Path

VALID: dict[str, Any] = {
    "probes": {"address": ":7070"},
    "log": {"level": "info"},
    "drain": {"seconds": 20},
    "events": {
        "nats": {"url": "nats://broker:4222"},
        "consumer": {
            "stream": "URL_SHORTENER",
            "durable": "log",
            "subject": "url-shortener.log",
        },
    },
    "archive": {
        "bucket": {"name": "archive", "endpoint": "http://s3:4566", "pathStyle": True},
        "prefix": "url-shortener/requests",
        "batch": {"maxRecords": 100, "maxSeconds": 30},
    },
}


def test_the_schema_travels_inside_the_package() -> None:
    # Not in ../../schemas/ with the Go binaries'. A wheel carries only what
    # is inside the package, so a schema a directory above is present in a
    # checkout and missing from the image — and every test still passes.
    assert config.SCHEMA.is_file()
    assert config.schema()["title"] == "log"


def test_a_valid_file_loads(tmp_path: Path) -> None:
    path = tmp_path / "log.yaml"
    path.write_text(yaml.safe_dump(VALID), encoding="utf-8")

    cfg = config.read(str(path))

    assert cfg["events"]["consumer"]["durable"] == "log"
    assert cfg["archive"]["bucket"]["name"] == "archive"


@pytest.mark.parametrize(
    ("remove", "key"),
    [(("probes",), "probes"), (("events",), "events"), (("archive",), "archive")],
)
def test_a_missing_section_is_refused(tmp_path: Path, remove: tuple[str], key: str) -> None:
    doc: dict[str, Any] = {k: v for k, v in VALID.items() if k not in remove}
    path = tmp_path / "log.yaml"
    path.write_text(yaml.safe_dump(doc), encoding="utf-8")

    with pytest.raises(ConfigError) as refusal:
        config.read(str(path))

    assert key in str(refusal.value)


def test_an_unknown_key_is_refused_by_name() -> None:
    # The most common configuration mistake there is: a key that was renamed,
    # or one somebody expected this component to read. Accepting it silently
    # is what turns a typo into a default nobody chose.
    doc = {**VALID, "archive": {**VALID["archive"], "compression": "gzip"}}

    with pytest.raises(ConfigError) as refusal:
        validate(doc, config.schema())

    assert "compression" in str(refusal.value)


def test_the_bucket_carries_no_credential_values() -> None:
    # Only the NAMES of variables. A value here would be printed by every
    # rendering, every describe and every debugging session.
    doc = {
        **VALID,
        "archive": {
            **VALID["archive"],
            "bucket": {**VALID["archive"]["bucket"], "accessKey": "AKIAEXAMPLE"},
        },
    }

    with pytest.raises(ConfigError):
        validate(doc, config.schema())

    named = {
        **VALID,
        "archive": {
            **VALID["archive"],
            "bucket": {
                **VALID["archive"]["bucket"],
                "credentialsEnv": {
                    "accessKeyID": "S3_ACCESS_KEY_ID",
                    "secretAccessKey": "S3_SECRET_ACCESS_KEY",
                },
            },
        },
    }
    validate(named, config.schema())


def test_a_batch_limit_below_one_is_refused() -> None:
    doc = {
        **VALID,
        "archive": {**VALID["archive"], "batch": {"maxRecords": 0}},
    }

    with pytest.raises(ConfigError):
        validate(doc, config.schema())
