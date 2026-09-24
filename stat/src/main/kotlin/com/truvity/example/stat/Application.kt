package com.truvity.example.stat

import com.fasterxml.jackson.databind.ObjectMapper
import io.nats.client.Connection
import io.nats.client.JetStreamSubscription
import io.nats.client.PullSubscribeOptions
import java.time.Duration
import kotlinx.coroutines.runBlocking
import org.slf4j.LoggerFactory
import org.springframework.boot.SpringApplication
import org.springframework.context.ApplicationContextInitializer
import org.springframework.context.ConfigurableApplicationContext
import org.springframework.boot.autoconfigure.SpringBootApplication
import org.springframework.boot.context.event.ApplicationReadyEvent
import org.springframework.context.annotation.Bean
import org.springframework.context.event.EventListener
import org.springframework.boot.actuate.health.Health
import org.springframework.boot.actuate.health.HealthIndicator
import org.springframework.stereotype.Component

/**
 * The composition root.
 *
 * Read this file top to bottom and you know what the process is made of and
 * what it talks to. The framework wires ITSELF — that is what a framework is
 * for — and this service's own dependencies are constructed here, where they
 * can be read. See decision 0001.
 */
@SpringBootApplication
class StatApplication {
    // Not named `broker`: that name belongs to the health indicator below,
    // because a health group names the CONTRIBUTOR, and the contributor's
    // name is its bean name. Two beans cannot share one.
    @Bean
    fun natsConnection(config: Config): Connection = connect(config.events.nats)

    @Bean
    fun counter(config: Config): urlshortener.v1.UrlsServiceClient =
        urlsClient(config.urlsAddress, config.tls)
}

/**
 * Readiness reports whether this component can do its work, which is consume.
 *
 * It does NOT check the URL service. A service that is down is not this
 * component's outage to report, and a probe that checked it would take the
 * counter out of rotation for somebody else's problem — while the messages
 * piled up in the stream either way.
 *
 * Liveness checks nothing at all, which is the framework's default and the
 * right one: a liveness probe that checks a dependency restarts a healthy
 * process because something else is down, turning one outage into two.
 */
@Component("broker")
class BrokerHealth(private val broker: Connection) : HealthIndicator {
    override fun health(): Health =
        if (broker.status == Connection.Status.CONNECTED) {
            Health.up().build()
        } else {
            Health.down().withDetail("reason", "not connected to the event stream").build()
        }
}

@Component
class Consuming(
    private val config: Config,
    private val broker: Connection,
    private val counter: urlshortener.v1.UrlsServiceClient,
) {
    private val log = LoggerFactory.getLogger(javaClass)
    private val mapper = ObjectMapper()

    @Volatile private var running = true
    private var worker: Thread? = null

    @EventListener(ApplicationReadyEvent::class)
    fun start() {
        val stream = broker.jetStream()
        // The stream exists already — a component does not create the
        // stream it reads, because two components disagreeing about a
        // stream's retention is a data-loss argument nobody wins at run
        // time.
        val subscription: JetStreamSubscription =
            stream.subscribe(
                config.events.consumer.subject,
                PullSubscribeOptions.builder()
                    .stream(config.events.consumer.stream)
                    .configuration(consumerConfiguration(config.events.consumer))
                    .build(),
            )

        worker =
            Thread {
                while (running) {
                    val messages = subscription.fetch(BATCH, Duration.ofSeconds(1))
                    for (message in messages) {
                        val detailType = message.headers?.getFirst("X-Detail-Type")
                        val redirect = decode(detailType, String(message.data), mapper)
                        if (redirect == null) {
                            // Nothing this component reads. Acknowledged, so
                            // it is not redelivered forever.
                            message.ack()
                            continue
                        }
                        try {
                            runBlocking { recordClick(counter, redirect.longUrl) }
                            message.ack()
                            log.info("counted a redirect for {}", redirect.urlKey)
                        } catch (e: Exception) {
                            // No ack: let it be redelivered. A click counted
                            // zero times is worse than one counted twice,
                            // and the increment is by URL.
                            log.error("not counted, will be redelivered: {}", e.message)
                        }
                    }
                }
            }
        worker?.start()
        log.info("consuming {} as {}", config.events.consumer.subject, config.events.consumer.durable)
    }

    /**
     * SIGTERM drains: the loop stops fetching, and what was already pulled
     * is finished rather than abandoned to redelivery.
     */
    @jakarta.annotation.PreDestroy
    fun drain() {
        running = false
        worker?.join(Duration.ofSeconds(config.drainSeconds.toLong()).toMillis())
        broker.drain(Duration.ofSeconds(config.drainSeconds.toLong()))
        log.info("drained")
    }

    private companion object {
        const val BATCH = 50
    }
}

/** The configuration file, from the flag or the variable, as everywhere else. */
fun configPath(args: Array<String> = emptyArray()): String {
    val flag = args.indexOfFirst { it == "-config" || it == "--config" }
    if (flag >= 0 && flag + 1 < args.size) return args[flag + 1]
    return System.getenv("CONFIG_FILE")
        ?: error("no configuration file: pass -config or set CONFIG_FILE")
}

fun main(args: Array<String>) {
    // Read and validated BEFORE the framework starts, for two reasons. The
    // framework reads its own logging settings before any bean exists, so
    // the level has to be in place by then. And a configuration this
    // service will refuse should be refused in one line on stderr — not as
    // a framework stack trace about a bean that could not be created, which
    // is the same refusal with the answer buried in it.
    val config =
        try {
            readConfig(configPath(args))
        } catch (e: Exception) {
            System.err.println(e.message)
            kotlin.system.exitProcess(1)
        }
    System.setProperty("LOG_LEVEL", config.logLevel)

    // Registered rather than read again by a bean. The file is read ONCE,
    // by the composition root, which is the thing that knows where it came
    // from — a bean that re-read it would have to rediscover the path, and
    // the first version of this did exactly that and found nothing, because
    // the path arrives as an argument and a bean has none.
    SpringApplication(StatApplication::class.java).apply {
        addInitializers(
            ApplicationContextInitializer<ConfigurableApplicationContext> { ctx ->
                ctx.beanFactory.registerSingleton("configuration", config)
            },
        )
    }.run(*args)
}
