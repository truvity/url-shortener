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
  readonly failures: readonly string[];

  constructor(message: string, options: { file?: string; failures?: string[]; cause?: unknown } = {}) {
    const { file, failures = [], cause } = options;
    const detail = failures.length > 0 ? `${message}:\n  ${failures.join("\n  ")}` : message;
    super(file ? `configuration ${file} ${detail}` : `configuration ${detail}`, cause ? { cause } : {});
    this.name = "ConfigError";
    this.file = file;
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
 */
export function load<T>(path: string, schema: object): T {
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

  const failures = check(doc, schema);
  if (failures.length > 0) {
    throw new ConfigError("is not valid", { file: path, failures });
  }
  return doc as T;
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
