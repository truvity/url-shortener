// The Kotlin loader: one file, one schema, secrets by name.
//
// The same three calls as the Go, TypeScript and Python loaders, read
// against the SAME fixtures. That is the point rather than a detail — a
// contract whose implementations are tested against different inputs is
// several contracts wearing one name.
plugins {
    kotlin("jvm") version "2.2.20"
}

group = "com.truvity"

// A placeholder that never moves. The git TAG is the sole version authority
// and the release stamps it in — see hack/stamp-version.py.
version = "0.0.0"

repositories {
    mavenCentral()
}

kotlin {
    // The JVM this compiles FOR, which is not the JVM Gradle runs ON.
    // Declared, so that it is the environment manifest's pin rather than
    // whatever the daemon happens to be.
    jvmToolchain(21)

    compilerOptions {
        // Platform types from Java libraries become nullable, which is where
        // the null errors are. Without it a Java method returning null
        // type-checks as non-null Kotlin and fails at the call site.
        freeCompilerArgs.add("-Xjsr305=strict")
        allWarningsAsErrors.set(true)
    }
}

dependencies {
    // Jackson for both JSON and YAML, because a configuration file is YAML
    // and a schema is JSON and one parser for both means one set of
    // surprises. The canon names Jackson rather than a Kotlin-native
    // serialiser: every Java library in reach already speaks it.
    implementation("com.fasterxml.jackson.core:jackson-databind:2.20.0")
    implementation("com.fasterxml.jackson.dataformat:jackson-dataformat-yaml:2.20.0")
    implementation("com.fasterxml.jackson.module:jackson-module-kotlin:2.20.0")
    implementation("com.networknt:json-schema-validator:1.5.9")

    testImplementation(kotlin("test"))
    testImplementation("org.junit.jupiter:junit-jupiter:5.14.0")
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
}

// The shared schemas are COPIED into the jar rather than generated into a
// source file, which is what the TypeScript and Python loaders do instead.
//
// Either shape satisfies the rule — validation needs no network — and the
// copy is the better one here because a jar's resources are already a
// directory of files: generating Kotlin source would mean a second
// representation to keep in step, and `just drift` would have a fourth thing
// to check. There is one copy, and it is made at build time from the source
// of truth.
val carrySchemas by tasks.registering(Copy::class) {
    from(rootProject.layout.projectDirectory.dir("../schemas"))
    into(layout.buildDirectory.dir("carried-schemas/schemas"))
    include("**/*.json")
}

sourceSets {
    main {
        resources.srcDir(carrySchemas.map { it.destinationDir.parentFile })
    }
}

tasks.test {
    useJUnitPlatform()
    testLogging {
        events("failed")
        showStackTraces = true
        exceptionFormat = org.gradle.api.tasks.testing.logging.TestExceptionFormat.FULL
    }
}
