# `com.truvity:policy`

Load a service's configuration: read one file, validate it against a schema,
return it, and stop.

The same three calls as the Go, TypeScript and Python loaders in this
repository, against the same fixtures, wording their refusals the same way —
so that a misconfiguration reads identically whichever runtime refused it.

```kotlin
import com.truvity.policy.load
import com.truvity.policy.secret

val cfg = load("config.yaml", schema)              // validates, then returns
val password = secret(cfg.at("/database/passwordEnv").asText())  // the NAME, never a value
```

## Two things that are different here, and why

**The shared schemas are COPIED into the jar**, not generated into a source
file the way the TypeScript and Python loaders carry theirs. Either shape
satisfies the rule — validation needs no network — and a jar's resources are
already a directory of files, so generating Kotlin source would mean a second
representation to keep in step and a fourth thing for `just drift` to check.
There is one copy and the build makes it from the source of truth.

**There is no Gradle wrapper.** The environment manifest declares Gradle, so
a checked-in wrapper script would download a second copy and raise the same
"which one actually runs" question the Node toolchain already answered. The
JDK that *compiles* is declared as a toolchain rather than inherited from
whichever JVM Gradle's daemon happens to be running on — those are not the
same thing, and Gradle ships with its own.

See [`docs/contracts/config.md`](../docs/contracts/config.md) for the
contract this implements, and [`docs/canon/kotlin.md`](../docs/canon/kotlin.md)
for the library list.
