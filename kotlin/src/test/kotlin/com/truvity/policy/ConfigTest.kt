package com.truvity.policy

import java.nio.file.Files
import java.nio.file.Path
import kotlin.test.Test
import kotlin.test.assertContains
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

/**
 * The Go loader's own fixtures, which are the shared ones.
 *
 * Every loader in this repository is read against these. That is the point
 * rather than a convenience: a contract with one implementation is a
 * library, and a contract whose implementations are tested against different
 * inputs is several contracts wearing one name. A key one loader refuses and
 * another accepts is a configuration that passes a chart's tests and crashes
 * the service.
 */
private val FIXTURES: Path = Path.of("..", "config", "testdata")

private fun fixture(name: String): String = FIXTURES.resolve(name).toString()

private fun schema(): String = Files.readString(FIXTURES.resolve("shortener.schema.json"))

class ConfigTest {
    @Test
    fun `a valid file loads`() {
        val cfg = load(fixture("valid.yaml"), schema())

        assertEquals(":8080", cfg.at("/listen/address").asText())
        assertEquals(":7070", cfg.at("/probes/address").asText())
        assertEquals(20, cfg.at("/database/maxConnections").asInt())
    }

    @Test
    fun `an unknown key is refused by name`() {
        // The most common configuration mistake there is: a key that was
        // renamed, or one somebody expected this service to read. Accepting
        // it silently turns a typo into a default nobody chose.
        val refusal = assertFailsWith<ConfigError> { load(fixture("unknown-key.yaml"), schema()) }

        assertContains(refusal.message!!, "not a key this service reads")
    }

    @Test
    fun `a missing required key is refused by name`() {
        val refusal = assertFailsWith<ConfigError> { load(fixture("missing-required.yaml"), schema()) }

        assertContains(refusal.message!!, "missing property")
    }

    @Test
    fun `a wrong type is refused`() {
        val refusal = assertFailsWith<ConfigError> { load(fixture("wrong-type.yaml"), schema()) }

        assertContains(refusal.message!!, "maxConnections")
    }

    @Test
    fun `an empty file is refused`() {
        val refusal = assertFailsWith<ConfigError> { load(fixture("empty.yaml"), schema()) }

        assertContains(refusal.message!!, "is empty")
    }

    @Test
    fun `a file that is not there is refused, naming it`() {
        val refusal = assertFailsWith<ConfigError> { load(fixture("nothing-here.yaml"), schema()) }

        assertContains(refusal.message!!, "nothing-here.yaml")
        assertContains(refusal.message!!, "cannot be read")
    }

    @Test
    fun `the refusal names the file and every failing key at once`() {
        // A person fixing a configuration wants every failing key in one
        // run, not one per restart.
        val refusal = assertFailsWith<ConfigError> { load(fixture("unknown-key.yaml"), schema()) }

        assertContains(refusal.message!!, "unknown-key.yaml")
    }

    @Test
    fun `validate checks a document that was never a file`() {
        // What a chart's tests call: the rendered configuration is held to
        // the same schema the binary reads at start-up, without either
        // having been written to disk.
        validate(load(fixture("valid.yaml"), schema()), schema())
    }

    @Test
    fun `a password written into the file is refused, and the refusal does not repeat it`() {
        // A configuration file is mounted from a config map, printed when
        // somebody debugs a deployment, and committed as a fixture. It has
        // to survive all three being true — and so does the REFUSAL, which
        // otherwise puts the value in every log that records it.
        val refusal = assertFailsWith<ConfigError> { load(fixture("secret-in-file.yaml"), schema()) }

        assertContains(refusal.message!!, "password")
        assertTrue(
            !refusal.message!!.contains("hunter2"),
            "the error quoted the value it refused:\n${refusal.message}",
        )
    }

    @Test
    fun `a reference to a published shape resolves without the network`() {
        // The fixture's schema references the shared envelope by an
        // identifier that looks like a URL. Nothing fetches it: the shapes
        // are carried in this jar, and a loader that reached out would make
        // start-up depend on a web server nobody runs.
        assertTrue(schema().contains(SCHEMA_BASE))
        load(fixture("valid.yaml"), schema())
    }

    @Test
    fun `secret reads the variable a configuration names`() {
        val name = System.getenv().keys.first { System.getenv(it).isNotEmpty() }

        assertEquals(System.getenv(name), secret(name))
    }

    @Test
    fun `an unset secret is refused, naming the variable and not the value`() {
        val refusal = assertFailsWith<ConfigError> { secret("DEFINITELY_NOT_SET_12345") }

        assertContains(refusal.message!!, "DEFINITELY_NOT_SET_12345")
        assertContains(refusal.message!!, "is not set")
    }

    @Test
    fun `naming no variable at all is refused`() {
        assertFailsWith<ConfigError> { secret("") }
    }

    @Test
    fun `the shared shapes are carried in the jar`() {
        // The same property the Go module gets from embedding: validation
        // needs no network.
        val envelope = schemaResource("/schemas/service.json")

        assertContains(envelope, "\"probes\"")
    }
}
