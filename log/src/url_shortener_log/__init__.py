"""The archiver: what the redirect service recorded, written to an object store.

It is the first Python component held to these contracts, and it is
deliberately the smallest thing that exercises them end to end: it reads one
configuration file, consumes a durable subject, batches, writes NDJSON to a
bucket, serves probes on their own port, and drains on SIGTERM.
"""
