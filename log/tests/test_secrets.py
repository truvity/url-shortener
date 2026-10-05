"""The archiver reads the secrets it names from files under secrets.root."""

from __future__ import annotations

from typing import TYPE_CHECKING

import pytest

from url_shortener_log.archive import read_secret

if TYPE_CHECKING:
    from pathlib import Path

    from url_shortener_log.config import Secrets


def test_a_secret_is_read_from_a_file_under_the_root(tmp_path: Path) -> None:
    (tmp_path / "s3").mkdir()
    (tmp_path / "s3" / "key").write_text("value\n", encoding="utf-8")

    secrets: Secrets = {"source": "file", "root": str(tmp_path)}
    assert read_secret(secrets, "f", "s3/key") == "value"


@pytest.mark.parametrize("name", ["../x", "/etc/passwd", "a//b", ".", "a\n", "ok\n"])
def test_a_name_that_climbs_is_refused_without_quoting_it(tmp_path: Path, name: str) -> None:
    secrets: Secrets = {"source": "file", "root": str(tmp_path)}
    with pytest.raises(ValueError, match="not a secret name") as err:
        read_secret(secrets, "f", name)
    assert name not in str(err.value)


def test_a_missing_or_empty_secret_is_refused(tmp_path: Path) -> None:
    (tmp_path / "empty").write_text("", encoding="utf-8")
    secrets: Secrets = {"source": "file", "root": str(tmp_path)}

    with pytest.raises(ValueError, match="not readable"):
        read_secret(secrets, "f", "missing")
    with pytest.raises(ValueError, match="empty"):
        read_secret(secrets, "f", "empty")


@pytest.mark.parametrize("secrets", [None, {"source": "env"}, {"source": "ssm", "root": "/x"}])
def test_a_source_other_than_file_is_refused(secrets: Secrets | None) -> None:
    with pytest.raises(ValueError, match=r"secrets\.source file"):
        read_secret(secrets, "f", "a")


@pytest.mark.parametrize("root", ["", "relative", "/a/../b", "/a//b", "/a/", "/a\n"])
def test_a_root_that_is_not_clean_is_refused(root: str) -> None:
    secrets: Secrets = {"source": "file", "root": root}
    with pytest.raises(ValueError, match=r"secrets\.root"):
        read_secret(secrets, "f", "a")
