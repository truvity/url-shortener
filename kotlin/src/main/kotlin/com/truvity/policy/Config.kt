/**
 * Load a service's configuration: read one file, validate it against a
 * schema, decode it into a type, and stop.
 *
 * That is the whole of it, deliberately. No lifecycle, no dependency wiring,
 * no HTTP, no reflection over the environment — those are what a
 * configuration package grows into when nobody says it must not, and a
 * service that depends on them cannot be understood without them.
 *
 * The contract this implements is docs/contracts/config.md. The Go,
 * TypeScript and Python loaders in this repository implement the same one,
 * against the same fixtures, and word their refusals the same way: a
 * misconfiguration reads identically whichever runtime refused it.
 */
package com.truvity.policy

import com.fasterxml.jackson.databind.JsonNode
import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.dataformat.yaml.YAMLFactory
import com.fasterxml.jackson.module.kotlin.registerKotlinModule
import com.networknt.schema.JsonSchema
import com.networknt.schema.JsonSchemaFactory
import com.networknt.schema.SchemaId
import com.networknt.schema.SchemaLocation
import com.networknt.schema.SpecVersion
import com.networknt.schema.ValidationMessage
import java.io.IOException
import java.nio.file.Files
import java.nio.file.Path

/** The identifier every shape this repository publishes is registered under. */
const val SCHEMA_BASE: String = "https://github.com/truvity/policy/schemas/"

/**
 * What [load], [validate] and [secret] throw.
 *
 * It names the file and every failing path, and never contains a value from
 * the file: a configuration sits next to the NAME of a secret, errors are
 * logged, and a message that quoted what it refused would put it in every
 * log that records the refusal.
 */
class ConfigError(message: String, cause: Throwable? = null) : Exception(message, cause)

/**
 * Reads the configuration file at [path], validates it against [schema], and
 * returns it as a tree.
 *
 * The file is YAML, which means JSON is accepted too. Validation happens
 * BEFORE anything is returned, so a caller never sees a value the schema
 * would have rejected.
 *
 * Any `$ref` to a shape this repository publishes resolves from the copies
 * carried in this jar; nothing is fetched.
 *
 * It reads version 1 of a document and nothing else: a document whose
 * `apiVersion` names a later version is refused before it is validated,
 * rather than checked against a schema it was not written for. Reading N and
 * N-1 (docs/contracts/config.md, rule 7) is in the Go and TypeScript loaders
 * and not yet here.
 */
fun load(path: String, schema: String): JsonNode {
    val raw =
        try {
            Files.readString(Path.of(path))
        } catch (e: IOException) {
            throw ConfigError("configuration $path cannot be read", e)
        }

    val document =
        try {
            yaml.readTree(raw)
        } catch (e: IOException) {
            throw ConfigError("configuration $path is not valid YAML", e)
        }

    // Three ways to be empty, and they are not the same value. A file with
    // nothing in it parses to a MISSING node; a file holding only `null`
    // parses to a null one. Checking only the second lets the first reach
    // the validator, which reports "unknown found, object expected" —
    // accurate, and no help at all to somebody looking at a blank file.
    if (document == null || document.isNull || document.isMissingNode) {
        throw ConfigError("configuration $path is empty")
    }

    versionFailure(document)?.let { throw ConfigError("configuration $path is not valid:\n  $it") }

    val failures = check(document, schema)
    if (failures.isNotEmpty()) {
        throw ConfigError("configuration $path is not valid:\n  " + failures.joinToString("\n  "))
    }
    return document
}

/**
 * Checks an already-parsed document against [schema], throwing on failure.
 *
 * Exported because a chart's tests validate what they render with the same
 * call, which is what stops the two drifting.
 */
fun validate(document: JsonNode, schema: String) {
    val failures = check(document, schema)
    if (failures.isNotEmpty()) {
        throw ConfigError("configuration is not valid:\n  " + failures.joinToString("\n  "))
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
fun secret(name: String): String {
    if (name.isEmpty()) {
        throw ConfigError("no environment variable was named for this secret")
    }
    val value = System.getenv(name) ?: throw ConfigError("environment variable $name is not set")
    if (value.isEmpty()) {
        throw ConfigError("environment variable $name is empty")
    }
    return value
}

/** The form of an apiVersion, the same pattern the envelope schema holds. */
private val apiVersionForm =
    Regex("([a-z0-9]([a-z0-9.-]*[a-z0-9])?/[a-z0-9]([a-z0-9-]*[a-z0-9])?)/v([1-9][0-9]*)")

/**
 * Why [document] is not a version 1 document, or null. Worded as the Go
 * loader words it, and never quoting the value unless it is of the form.
 */
private fun versionFailure(document: JsonNode): String? {
    if (!document.isObject || !document.has("apiVersion")) return null
    val raw = document.get("apiVersion")
    val match =
        (if (raw.isTextual) apiVersionForm.matchEntire(raw.asText()) else null)
            ?: return "apiVersion: not of the form <group>/<kind>/v<N>"
    val got = match.groupValues[4].toInt()
    return if (got > 1) "apiVersion: v$got is newer than this binary reads (v1)" else null
}

private val yaml = ObjectMapper(YAMLFactory()).registerKotlinModule()
private val json = ObjectMapper().registerKotlinModule()

private val factory =
    JsonSchemaFactory.getInstance(SpecVersion.VersionFlag.V202012) { builder ->
        // The shapes this repository publishes, resolved from the jar. The
        // identifiers are names, not addresses: nothing fetches them, and a
        // loader that did would make a service's start-up depend on a web
        // server nobody runs.
        builder.schemaMappers { mappers ->
            mappers.mapPrefix(SCHEMA_BASE, "classpath:schemas/")
        }
    }

private fun check(document: JsonNode, schema: String): List<String> {
    val compiled: JsonSchema =
        try {
            factory.getSchema(schema)
        } catch (e: RuntimeException) {
            throw ConfigError("the schema itself is not valid", e)
        }
    return compiled.validate(document).map(::describe).distinct().sorted()
}

/**
 * Turns one validator message into the line a person reads.
 *
 * "invalid config" is not an error message: the reader is looking at a file
 * and needs the key. The wording matches the other loaders', so that the
 * same misconfiguration reads the same way whichever runtime refused it.
 */
private fun describe(message: ValidationMessage): String {
    val path = pointerToPath(message.instanceLocation.toString())
    return when (message.type) {
        // The most common configuration mistake there is, and the
        // specification's own word for it ("additionalProperties") is
        // accurate and meaningless to anyone who has not read it.
        "additionalProperties", "unevaluatedProperties" -> {
            val key = message.property ?: message.arguments?.firstOrNull()?.toString()
            val where = if (path == "(root)") "" else "$path."
            "$where$key: not a key this service reads"
        }
        "required" -> {
            val key = message.property ?: message.arguments?.firstOrNull()?.toString()
            "$path: missing property '$key'"
        }
        // A `not` in a schema here is a form a value must not take (a
        // password in a database URL); the schema's description of it says
        // which and why. The other loaders' words.
        "not" -> "$path: is in a form the schema forbids here (its description says why)"
        else -> "$path: ${message.message.substringAfter(": ", message.message)}"
    }
}

/** `/database/maxConnections` becomes `database.maxConnections`. */
private fun pointerToPath(pointer: String): String {
    if (pointer.isEmpty() || pointer == "$") return "(root)"
    return pointer.removePrefix("$").removePrefix(".").replace("[", ".").replace("]", "")
}

/** Reads a schema from the classpath, the way a binary carries its own. */
fun schemaResource(name: String): String =
    object {}.javaClass.getResourceAsStream(name)?.use { it.readBytes().decodeToString() }
        ?: throw ConfigError("schema $name is not carried by this build")

/** Parses a JSON document, for a caller holding one in memory. */
fun parse(text: String): JsonNode = json.readTree(text)
