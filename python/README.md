# `truvity-policy`

Load a service's configuration: read one file, validate it against a schema,
return it, and stop.

The same three calls as the Go and TypeScript loaders in this repository,
against the same fixtures, wording their refusals the same way — so that a
misconfiguration reads identically whichever runtime refused it.

```python
from truvity_policy import ConfigError, load, secret

cfg = load("config.yaml", schema)  # validates, then returns
name = cfg["database"]["passwordSecret"]  # the NAME, never a value
password = secret(name)  # reads the env source only
```

See [`docs/contracts/config.md`](../docs/contracts/config.md) for the
contract this implements, and [`docs/canon/python.md`](../docs/canon/python.md)
for the library list.
