# Contributing

Pull requests are welcome, from forks too.

- `just check` is the gate and runs the same recipes CI runs; `devbox shell`
  (or direnv) gives you the toolchain the lock file names.
- A pull request from a fork runs the plain check only, on a GitHub-hosted
  runner with a read-only token and no secret. The first run waits for a
  maintainer's approval, as every run from outside the organization does.
- The integration suite (`docs/integration-testing.md`) reaches a test cluster
  and is not run for pull requests from forks. If your change needs it, a
  maintainer reads the diff and runs it from a branch of this repository.
- Generated code is committed: run `just protos`, `just schemas`,
  `just chart-schemas` and `just golden` as the change needs, and read the diff.
- Contracts, shared schemas and the library chart belong to
  [truvity/policy](https://github.com/truvity/policy); change them there.
- Do not put estate particulars (account ids, host names, bucket names) in a
  file: `just leak-canary` refuses them.
