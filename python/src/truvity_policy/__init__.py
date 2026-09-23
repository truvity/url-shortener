"""Load a service's configuration: one file, one schema, secrets by name."""

from .config import ConfigError, load, secret, validate
from .schemas import SCHEMA_BASE, SHARED_SCHEMAS

__all__ = ["SCHEMA_BASE", "SHARED_SCHEMAS", "ConfigError", "load", "secret", "validate"]
