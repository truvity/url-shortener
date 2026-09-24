// The click counter, in Kotlin.
//
// It consumes what the redirect service published and asks the service that
// owns the table to count it. It holds no database credential: the table is
// somebody else's property, and this component has an address.
plugins {
    kotlin("jvm") version "2.2.20"
    kotlin("plugin.spring") version "2.2.20"
    id("org.springframework.boot") version "3.5.6"
    id("io.spring.dependency-management") version "1.1.7"
    id("com.google.protobuf") version "0.9.5"
}

group = "com.truvity.example"
version = "0.0.0"

repositories { mavenCentral() }

kotlin {
    jvmToolchain(21)
    compilerOptions {
        freeCompilerArgs.add("-Xjsr305=strict")
        allWarningsAsErrors.set(true)
    }
}

dependencies {
    // This repository's loader: one file, one schema, secrets by name.
    implementation("com.truvity:policy")

    // The framework, because the estate's existing JVM service is a Spring
    // Boot service and a second framework for one component is how two
    // become permanent. What it is actually used for here is narrow: the
    // probe listener on its own port, and a graceful shutdown.
    implementation("org.springframework.boot:spring-boot-starter-web")
    implementation("org.springframework.boot:spring-boot-starter-actuator")
    implementation("com.fasterxml.jackson.module:jackson-module-kotlin")

    // Telemetry, configured by OpenTelemetry's own environment (decision
    // 0006). The starter reads those variables itself -- including
    // OTEL_TRACES_EXPORTER=none, which is what a deployment with no
    // endpoint sets -- so nothing here tests an enable flag, and the
    // instrumentation for the web layer comes with it.
    implementation("io.opentelemetry.instrumentation:opentelemetry-spring-boot-starter:2.11.0")

    // The broker's own client.
    implementation("io.nats:jnats:2.23.0")

    // Connect, as a CLIENT. The Kotlin library generates no servers, which
    // is a real constraint on where a JVM service sits in a topology and is
    // written down in docs/canon/kotlin.md.
    implementation("com.connectrpc:connect-kotlin:0.7.4")
    implementation("com.connectrpc:connect-kotlin-okhttp:0.7.4")
    implementation("com.connectrpc:connect-kotlin-google-java-ext:0.7.4")
    implementation("com.google.protobuf:protobuf-java:4.33.0")

    testImplementation("org.springframework.boot:spring-boot-starter-test")
    testImplementation(kotlin("test"))
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
}

tasks.test { useJUnitPlatform() }

// One name, so the Dockerfile does not have to know the version. The TAG is
// the version authority and the image is stamped by the registry reference,
// not by a filename inside it.
tasks.named<org.springframework.boot.gradle.tasks.bundling.BootJar>("bootJar") {
    archiveFileName.set("stat.jar")
}

// The Connect Kotlin generator is published as a plain JAR — no native
// executable, and no platform classifier. The protobuf plugin runs a
// COMMAND, so it gets one: a two-line launcher written at build time around
// the jar this configuration resolves.
//
// It is worth saying why this is not a workaround for something simpler.
// Every other generator in this repository is a static binary the
// environment manifest pins; this one is a JVM artifact, so the JVM's own
// dependency resolution is where it has to come from. Pinning it in the
// manifest would mean pinning a jar as if it were a binary, and pretending
// the JVM is not already on the machine.
val connectGenerator: Configuration by configurations.creating

dependencies {
    connectGenerator("com.connectrpc:protoc-gen-connect-kotlin:0.7.4")
}

val connectGeneratorScript = layout.buildDirectory.file("protoc-gen-connect-kotlin")

val writeConnectGenerator by tasks.registering {
    inputs.files(connectGenerator)
    outputs.file(connectGeneratorScript)
    doLast {
        // The whole classpath, not just the generator's own jar: it has
        // transitive dependencies, and `java -jar` ignores -cp.
        val classpath = connectGenerator.joinToString(":") { it.absolutePath }
        val script = connectGeneratorScript.get().asFile
        script.writeText(
            """
            #!/usr/bin/env bash
            exec java -cp "$classpath" com.connectrpc.protocgen.connect.Main "${'$'}@"
            """.trimIndent() + "\n",
        )
        script.setExecutable(true)
    }
}

protobuf {
    protoc { artifact = "com.google.protobuf:protoc:4.33.0" }
    plugins {
        create("connectkt") {
            path = connectGeneratorScript.get().asFile.absolutePath
        }
    }
    generateProtoTasks {
        all().forEach { task ->
            task.dependsOn(writeConnectGenerator)
            task.plugins { create("connectkt") }
        }
    }
}

// The schema is COPIED into the jar at build time, not committed here.
//
// A jar carries only its resources, so a schema a directory above would be
// present in a checkout and missing from the image — and every test would
// still pass. Copying keeps ONE copy: the source of truth stays in
// ../schemas/, where the chart's tests read it, and this build takes it
// from there.
val carrySchema by tasks.registering(Copy::class) {
    from(layout.projectDirectory.file("../schemas/stat.json")) { rename { "stat.schema.json" } }
    into(layout.buildDirectory.dir("carried-schema"))
}

sourceSets {
    main {
        proto { srcDir("../proto") }
        resources.srcDir(carrySchema)
    }
}
