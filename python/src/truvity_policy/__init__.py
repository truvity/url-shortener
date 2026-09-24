"""What this repository's contracts need at the edge of a Python process.

Two things, and they are the two a service cannot be written without:

- :mod:`~truvity_policy.config` — one file, one schema, secrets by name.
- :mod:`~truvity_policy.transport` — the identity the platform mounted, and
  who it admits.

The transport helper is NOT re-exported here. Importing it pulls in ``ssl``,
and a component that serves and makes only cleartext connections — which is
the default, and always will be — has no reason to. Reach for it as
``from truvity_policy.transport import load`` where it is needed.
"""

from .config import ConfigError, load, secret, validate
from .schemas import SCHEMA_BASE, SHARED_SCHEMAS

__all__ = ["SCHEMA_BASE", "SHARED_SCHEMAS", "ConfigError", "load", "secret", "validate"]
