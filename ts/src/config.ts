/**
 * Load a service's configuration: read one file, validate it against a
 * schema, decode it into a type, and stop.
 *
 * That is the whole of it, deliberately. No lifecycle, no dependency wiring,
 * no HTTP, no reflection over the environment — those are what a
 * configuration package grows into when nobody says it must not, and a
 * service that depends on them cannot be understood without them.
 *
 * The contract this implements is docs/contracts/config.md. The Go loader in
 * this repository implements the same one, against the same fixtures.
 */
import { readFileSync } from "node:fs";
import type { ErrorObject, ValidateFunction } from "ajv";
import { Ajv2020 } from "ajv/dist/2020.js";
import { parse } from "yaml";

import { sharedSchemas } from "./schemas.generated.js";

/**
 * What {@link load}, {@link validate} and {@link secret} throw.
 *
 * It names the file and every failing path, and never contains a value from
 * the file: a configuration sits next to the NAME of a secret, errors are
 * logged, and a message that quotes what it refused puts it in every log that
 * records the refusal.
 */
export class ConfigError extends Error {
  readonly file: string | undefined;
  /**
   * Which version of the document the failures are against, when the binary
   * reads versions: `as <group>/<kind>/vN`, or `upgraded from vN-1 to
   * <group>/<kind>/vN` when the document was converted first.
   */
  readonly as: string | undefined;
  readonly failures: readonly string[];

  constructor(
    message: string,
    options: { file?: string; as?: string; failures?: string[]; cause?: unknown } = {},
  ) {
    const { file, as, failures = [], cause } = options;
    const detail = failures.length > 0 ? `${message}:\n  ${failures.join("\n  ")}` : message;
    const where = (file ? ` ${file}` : "") + (as ? ` (${as})` : "");
    super(`configuration${where} ${detail}`, cause ? { cause } : {});
    this.name = "ConfigError";
    this.file = file;
    this.as = as;
    this.failures = failures;
  }
}

/**
 * Reads the configuration file at `path`, validates it against `schema`, and
 * returns it.
 *
 * The file is YAML, which means JSON is accepted too. Validation happens
 * BEFORE anything is returned, so a caller never sees a value the schema
 * would have rejected.
 *
 * Any `$ref` to a shape this repository publishes resolves from the copies
 * compiled into this package; nothing is fetched.
 *
 * It reads version 1 of a document and nothing else: a document whose
 * `apiVersion` names a later version is refused before it is validated,
 * rather than checked against a schema it was not written for. A binary that
 * reads a later version, or two, declares them with a {@link Kind} and calls
 * {@link loadKind}.
 */
export function load<T>(path: string, schema: object): T {
  const doc = read(path);
  const failure = versionFailure(doc, { version: 1 });
  if (failure) {
    throw new ConfigError("is not valid", { file: path, failures: [failure] });
  }
  const failures = check(doc, schema);
  if (failures.length > 0) {
    throw new ConfigError("is not valid", { file: path, failures });
  }
  return doc as T;
}

/** The key a document names its version under, at its root. Absent means v1. */
export const API_VERSION_KEY = "apiVersion";

/**
 * One kind of configuration document, and the versions of it a binary reads:
 * the current one, N, and optionally the one before, N-1.
 *
 * Two and no more, deliberately (docs/contracts/config.md, rule 7): a binary
 * that reads N-1 can be rolled out before its configuration moves to N, and a
 * binary that read every version would carry every conversion forever. Each
 * version has its own authored schema; nothing here generates one.
 */
export interface Kind {
  /** `<group>/<kind>`: the apiVersion without its version. */
  readonly name: string;
  /** N, the version this binary is written against. Absent means 1. */
  readonly version?: number;
  /** Version N's schema. */
  readonly schema: object;
  /** Version N-1's schema; absent when this binary reads N alone. */
  readonly previous?: object;
  /**
   * Turns a document valid against `previous` into one of version N. It is
   * given the whole document, apiVersion included; the loader then sets
   * apiVersion to N and validates the result against `schema`. Required
   * with `previous`. What it throws is reported with the file's name and is
   * logged: it must name keys, never quote values.
   */
  readonly upgrade?: (doc: Record<string, unknown>) => Record<string, unknown>;
}

/**
 * Reads the configuration file at `path` as a document of `kind`, and returns
 * it as version N.
 *
 * It reads the document's apiVersion before anything else and chooses the
 * schema by it: version N is validated against `schema`; version N-1, when
 * the binary reads it, is validated against `previous`, converted by
 * `upgrade`, and validated against `schema`; anything else is refused, naming
 * the key. A document with no apiVersion is version 1.
 */
export function loadKind<T>(path: string, kind: Kind): T {
  const declared = declarationFailure(kind);
  if (declared) {
    throw new ConfigError(declared, { file: path });
  }

  const doc = read(path);
  const failure = versionFailure(doc, kind);
  if (failure) {
    throw new ConfigError("is not valid", { file: path, failures: [failure] });
  }

  const n = kind.version ?? 1;
  const current = `${kind.name}/v${n}`;
  // A root that is not a mapping is checked against N's schema, which
  // refuses it naming the root: there is no version to choose by.
  if (!isMapping(doc) || documentVersion(doc) === n) {
    const failures = check(doc, kind.schema);
    if (failures.length > 0) {
      throw new ConfigError("is not valid", { file: path, as: `as ${current}`, failures });
    }
    return doc as T;
  }

  // N-1: versionFailure has already refused everything else.
  const previous = kind.previous as object;
  const upgrade = kind.upgrade as NonNullable<Kind["upgrade"]>;
  const asPrevious = check(doc, previous);
  if (asPrevious.length > 0) {
    throw new ConfigError("is not valid", {
      file: path,
      as: `as ${kind.name}/v${n - 1}`,
      failures: asPrevious,
    });
  }
  let upgraded: Record<string, unknown>;
  try {
    upgraded = upgrade(doc);
  } catch (cause) {
    const reason = cause instanceof Error ? cause.message : String(cause);
    throw new ConfigError(`upgrading v${n - 1} to v${n}: ${reason}`, { file: path, cause });
  }
  if (!isMapping(upgraded)) {
    throw new ConfigError(`upgrading v${n - 1} to v${n} returned no document`, { file: path });
  }
  upgraded[API_VERSION_KEY] = current;
  const failures = check(upgraded, kind.schema);
  if (failures.length > 0) {
    throw new ConfigError("is not valid", {
      file: path,
      as: `upgraded from v${n - 1} to ${current}`,
      failures,
    });
  }
  return upgraded as T;
}

/**
 * The path of the binary's one configuration file (docs/contracts/service.md,
 * rule 1): the argument `--config <path>` if the command line has it,
 * otherwise the environment variable `envName`.
 *
 * `args` is the command line without the runtime and the script,
 * `process.argv.slice(2)`. The argument is spelled `--config` or `-config`,
 * with the path as the next argument or after `=`; nothing after `--` is read
 * as an option. The argument wins over the variable. `envName` is the
 * service's own, conventionally `<APP>_CONFIG`.
 *
 * Every other argument is left alone. What it refuses is the config argument
 * with no value, the config argument given twice, an empty variable, and
 * neither at all.
 */
export function configPath(
  args: readonly string[],
  envName: string,
  env: Readonly<Record<string, string | undefined>> = process.env,
): string {
  let found: string | undefined;
  for (let i = 0; i < args.length; i++) {
    const a = args[i] as string;
    if (a === "--") break;
    let value: string;
    if (a === "--config" || a === "-config") {
      const next = args[i + 1];
      value = next !== undefined && !next.startsWith("-") ? next : "";
      if (value) i++;
    } else if (a.startsWith("--config=")) {
      value = a.slice("--config=".length);
    } else if (a.startsWith("-config=")) {
      value = a.slice("-config=".length);
    } else {
      continue;
    }
    if (!value) throw new ConfigError("--config has no value");
    if (found !== undefined) throw new ConfigError("--config is given more than once");
    found = value;
  }
  if (found !== undefined) return found;

  if (!envName) {
    throw new ConfigError("no configuration file: pass --config <path>");
  }
  const value = env[envName];
  if (value === undefined) {
    throw new ConfigError(`no configuration file: pass --config <path> or set ${envName}`);
  }
  if (value === "") {
    throw new ConfigError(`no configuration file: ${envName} is empty`);
  }
  return value;
}

function read(path: string): unknown {
  let raw: string;
  try {
    raw = readFileSync(path, "utf8");
  } catch (cause) {
    throw new ConfigError("cannot be read", { file: path, cause });
  }

  let doc: unknown;
  try {
    doc = parse(raw);
  } catch (cause) {
    throw new ConfigError("is not valid YAML", { file: path, cause });
  }
  if (doc === null || doc === undefined) {
    throw new ConfigError("is empty", { file: path });
  }
  return doc;
}

/**
 * The form of an apiVersion. The envelope schema (schemas/service.json) holds
 * the same pattern, so a chart's tests refuse what the loader refuses.
 */
const apiVersionForm = /^([a-z0-9]([a-z0-9.-]*[a-z0-9])?\/[a-z0-9]([a-z0-9-]*[a-z0-9])?)\/v([1-9][0-9]*)$/;
const kindForm = /^[a-z0-9]([a-z0-9.-]*[a-z0-9])?\/[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;

function isMapping(doc: unknown): doc is Record<string, unknown> {
  return typeof doc === "object" && doc !== null && !Array.isArray(doc);
}

/** The version a document says it is: 1 when it says nothing, 0 when what it says is not of the form. */
function documentVersion(doc: Record<string, unknown>): number {
  if (!(API_VERSION_KEY in doc)) return 1;
  const raw = doc[API_VERSION_KEY];
  const match = typeof raw === "string" ? apiVersionForm.exec(raw) : null;
  return match ? Number(match[4]) : 0;
}

/** Why a declaration cannot work, or undefined. The binary's mistake, not the file's. */
function declarationFailure(kind: Kind): string | undefined {
  const n = kind.version ?? 1;
  if (!kindForm.test(kind.name)) {
    return `the binary declares kind ${JSON.stringify(kind.name)}, which is not of the form <group>/<kind>`;
  }
  if (!Number.isInteger(n) || n < 1) return `the binary declares ${kind.name} at version ${n}`;
  if (!kind.schema) return `the binary declares ${kind.name} with no schema`;
  if (kind.previous && n < 2) return `the binary declares ${kind.name} v1 with a previous version`;
  if (kind.previous && !kind.upgrade) {
    return `the binary declares ${kind.name} v${n} with a previous version and no upgrade`;
  }
  if (!kind.previous && kind.upgrade) {
    return `the binary declares ${kind.name} v${n} with an upgrade and no previous version`;
  }
  return undefined;
}

/**
 * Why a document's apiVersion is not one `kind` reads, or undefined when it
 * is. A kind with no name is the plain loader's: it reads v1 of whatever the
 * document is. The wording is the Go loader's, word for word.
 */
function versionFailure(
  doc: unknown,
  kind: { name?: string; version?: number; previous?: object },
): string | undefined {
  const key = `${API_VERSION_KEY}: `;
  const n = kind.version ?? 1;
  const oldest = kind.previous ? n - 1 : n;
  const reads = kind.previous ? `v${n}, v${n - 1}` : `v${n}`;

  // Not a mapping at all: the schema says so, naming the root.
  if (!isMapping(doc)) return undefined;
  if (!(API_VERSION_KEY in doc)) {
    return oldest > 1 ? `${key}absent, which is v1, older than this binary reads (${reads})` : undefined;
  }
  const raw = doc[API_VERSION_KEY];
  const match = typeof raw === "string" ? apiVersionForm.exec(raw) : null;
  if (!match) return `${key}not of the form <group>/<kind>/v<N>`;
  if (kind.name && match[1] !== kind.name) {
    return `${key}names another kind of document; this binary reads ${kind.name}`;
  }
  const got = Number(match[4]);
  if (got > n) return `${key}v${got} is newer than this binary reads (${reads})`;
  if (got < oldest) return `${key}v${got} is older than this binary reads (${reads})`;
  return undefined;
}

/**
 * Checks an already-parsed document against `schema`, throwing on failure.
 *
 * Exported because a chart's tests validate what they render with the same
 * call, which is what stops the two drifting.
 */
export function validate(doc: unknown, schema: object): void {
  const failures = check(doc, schema);
  if (failures.length > 0) {
    throw new ConfigError("is not valid", { failures });
  }
}

/**
 * Reads the environment variable a configuration NAMES.
 *
 * A configuration file carries the name of the variable, never the value:
 * files are rendered into config maps, printed when somebody debugs a
 * deployment, and committed as test fixtures, and a secret has to survive all
 * three being true.
 *
 * An unset or empty variable throws, and the error names the variable rather
 * than quoting anything.
 */
export function secret(name: string): string {
  if (!name) {
    throw new ConfigError("no environment variable was named for this secret");
  }
  const value = process.env[name];
  if (value === undefined) {
    throw new ConfigError(`environment variable ${name} is not set`);
  }
  if (value === "") {
    throw new ConfigError(`environment variable ${name} is empty`);
  }
  return value;
}

const compiled = new WeakMap<object, ValidateFunction>();

function compile(schema: object): ValidateFunction {
  const cached = compiled.get(schema);
  if (cached) return cached;

  // `allErrors`: a person fixing a configuration wants every failing key at
  // once, not one per run.
  //
  // No format assertions, and that is a decision rather than an omission. In
  // draft 2020-12 `format` is an annotation unless a validator is told
  // otherwise, and the Go loader leaves it as one. A `format` that bites in
  // one runtime and not the other would mean a configuration this repository
  // accepts in a chart's tests and refuses in the service, which is the exact
  // drift these loaders exist to prevent. Where a rule must bite, the schema
  // says `pattern`.
  const ajv = new Ajv2020({ allErrors: true, strict: false });
  for (const [id, doc] of Object.entries(sharedSchemas)) {
    if ((schema as { $id?: string }).$id === id) continue;
    ajv.addSchema(doc, id);
  }
  const fn = ajv.compile(schema);
  compiled.set(schema, fn);
  return fn;
}

function check(doc: unknown, schema: object): string[] {
  let fn: ValidateFunction;
  try {
    fn = compile(schema);
  } catch (cause) {
    throw new ConfigError("cannot be checked: the schema itself is not valid", { cause });
  }
  if (fn(doc)) return [];
  return [...new Set((fn.errors ?? []).map(describe))].sort();
}

/**
 * Turns one validator error into the line a person reads.
 *
 * "invalid config" is not an error message: the reader is looking at a file
 * and needs the key. The wording matches the Go loader's, so that the same
 * misconfiguration reads the same way whichever runtime refused it.
 */
function describe(error: ErrorObject): string {
  const path = pointerToPath(error.instancePath);

  switch (error.keyword) {
    case "additionalProperties":
    case "unevaluatedProperties": {
      // The two keywords report the offending key under DIFFERENT parameter
      // names, and reading only one of them yields "undefined: not a key this
      // service reads" — a message that is worse than the specification's.
      const params = error.params as { additionalProperty?: string; unevaluatedProperty?: string };
      const key = params.additionalProperty ?? params.unevaluatedProperty;
      const where = path === "(root)" ? "" : `${path}.`;
      // The most common configuration mistake there is, so its message is
      // the one worth writing out rather than quoting the specification.
      return `${where}${key}: not a key this service reads`;
    }
    case "required": {
      const key = (error.params as { missingProperty?: string }).missingProperty;
      return `${path}: missing property '${key}'`;
    }
    case "type": {
      const want = (error.params as { type?: string }).type;
      return `${path}: got ${typeName(error.data)}, want ${want}`;
    }
    case "not":
      // "must NOT be valid" says nothing. A `not` in a schema here is a form
      // a value must not take (a password in a database URL); the schema's
      // description of it says which and why. The Go loader's words.
      return `${path}: is in a form the schema forbids here (its description says why)`;
    default:
      return `${path}: ${error.message ?? "does not satisfy the schema"}`;
  }
}

function typeName(value: unknown): string {
  if (value === null) return "null";
  if (Array.isArray(value)) return "array";
  if (typeof value === "number") return Number.isInteger(value) ? "integer" : "number";
  return typeof value;
}

/** `/database/maxConnections` becomes `database.maxConnections`. */
function pointerToPath(pointer: string): string {
  if (!pointer || pointer === "/") return "(root)";
  return pointer
    .slice(1)
    .split("/")
    .map((part) => part.replaceAll("~1", "/").replaceAll("~0", "~"))
    .join(".");
}
