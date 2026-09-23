"""What the probe listener answers, and what liveness deliberately does not check."""

from __future__ import annotations

import json
import socket
import urllib.error
import urllib.request

import pytest

from url_shortener_log.runtime import Probes, logger


def free_port() -> int:
    """Ask the kernel for a port nobody is using."""
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def get(port: int, path: str) -> tuple[int, str]:
    """Fetch one probe path, returning the status and the body."""
    try:
        url = f"http://127.0.0.1:{port}{path}"
        with urllib.request.urlopen(url, timeout=5) as response:
            return response.status, response.read().decode()
    except urllib.error.HTTPError as error:
        return error.code, error.read().decode()


def test_liveness_checks_nothing_so_a_dependency_outage_is_not_a_restart() -> None:
    # A liveness probe that checks a dependency restarts a healthy process
    # because something else is down, which turns one outage into two.
    port = free_port()
    probes = Probes(f"127.0.0.1:{port}", lambda: "the broker is unreachable")
    probes.start()
    try:
        assert get(port, "/health/live") == (200, "ok")
        code, body = get(port, "/health/ready")
        assert code == 503
        assert "broker" in body
    finally:
        probes.stop()


def test_readiness_passes_when_the_check_says_nothing_is_wrong() -> None:
    port = free_port()
    probes = Probes(f":{port}", lambda: None)
    probes.start()
    try:
        assert get(port, "/health/ready") == (200, "ok")
        assert get(port, "/anything-else")[0] == 404
    finally:
        probes.stop()


def test_logs_are_json_on_stderr_and_nothing_goes_to_stdout(
    capfd: pytest.CaptureFixture[str],
) -> None:
    # stdout is the program's product; stderr is its commentary. This
    # program's product is objects in a bucket, so stdout stays empty.
    log = logger("info")
    log.info("archived", key="p/1.ndjson", records=3)

    captured = capfd.readouterr()
    assert captured.out == ""
    line = json.loads(captured.err.strip().splitlines()[-1])
    assert line["event"] == "archived"
    assert line["records"] == 3
    assert line["level"] == "info"


def test_a_level_the_component_does_not_know_is_refused_by_name() -> None:
    with pytest.raises(ValueError, match="louder"):
        logger("louder")
