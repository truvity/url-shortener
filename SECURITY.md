# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please report it privately via
[GitHub Security Advisories](https://github.com/truvity/url-shortener/security/advisories/new).

Do NOT open a public issue for security vulnerabilities.

## Supported Versions

Only the latest release is supported with security updates.

## What is in scope

This repository publishes a worked example service (Go, Kotlin, TypeScript and
Python components, two Helm charts) and the small configuration loaders its
components use. Reports that matter most:

- A loader that accepts a configuration it should refuse, or that reports a
  secret's value in an error or a log line.
- Anything in the example that would be a vulnerability in a real service,
  since the example is what people copy.
- A way for a pull request from outside the organization to start a run that
  reaches anything but a read-only check: the integration workflow is meant to
  be startable only from this repository's `master`, a release tag or a manual
  dispatch of `master`, and a run from a fork gets no token to exchange.

This repository holds no credentials. Its plain check runs on hosted runners
with a read-only token; its integration run reaches a test cluster with a
short-lived token it exchanges for itself, and only from the runs above. A
finding that depends on a particular deployment belongs with that
deployment's owner. Contract and schema findings belong to
[truvity/policy](https://github.com/truvity/policy/security/advisories/new).
