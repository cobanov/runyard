package dev.cobanov.runyard

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import org.json.JSONTokener
import java.math.BigDecimal
import java.math.MathContext
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder
import java.time.OffsetDateTime
import java.util.Locale
import kotlin.math.abs
import kotlin.math.floor
import kotlin.math.log10

// The runyard REST API (server/api.go), as ios/Runyard/API.swift reads it: the app reads, and deletes runs and
// projects; it never writes runs.

data class Me(val auth: Boolean, val user: String)

data class Project(val name: String, val runs: Int, val running: Int, val lastRunAt: Long?)

data class MetricSummary(val count: Int, val step: Int, val last: Double, val min: Double, val max: Double)

data class Run(
    val id: String,
    val project: String,
    val name: String,
    val status: String,
    val config: JSONObject?,
    val summary: JSONObject?,
    val tags: List<String>,
    val notes: String,
    val host: String,
    val git: JSONObject?,
    val startedAt: Long,
    val endedAt: Long?,
    val lastHeartbeatAt: Long?,
    val metrics: Map<String, MetricSummary>?, // only with metrics=1 or from GET /runs/{id}
) {
    val running get() = status == "running"

    /** What a row shows, as the web UI picks it: the shortest key with "loss" in it, else the most logged key. */
    val mainMetric: Pair<String, Double>?
        get() {
            val m = metrics?.filterKeys { !it.startsWith("sys/") }.orEmpty()
            if (m.isEmpty()) return null
            val key = m.keys.filter { "loss" in it }.minWithOrNull(compareBy({ it.length }, { it }))
                ?: m.keys.sorted().maxBy { m.getValue(it).count }
            return key to m.getValue(key).last
        }

    /** How long it ran: until it ended, until now while running, else until its last heartbeat (a crash). */
    fun duration(now: Long = System.currentTimeMillis()) =
        ((endedAt ?: if (running) now else lastHeartbeatAt ?: startedAt) - startedAt) / 1000.0

    data class Progress(val done: Double, val total: Double, val unit: String)

    /**
     * How far it is, when the config names a total: steps (HF max_steps and the like) against the latest logged step,
     * else epochs against an epoch metric. Nested config keys count by their last part.
     */
    val progress: Progress?
        get() {
            val cfg = mutableMapOf<String, Double>()
            for ((k, v) in flat(config)) {
                val d = v.toDoubleOrNull() ?: continue
                cfg.merge(k.substringAfterLast('.'), d, ::maxOf) // sampler.steps must not hide train.steps
            }
            fun total(vararg names: String) = names.firstNotNullOfOrNull { n -> cfg[n]?.takeIf { it > 0 } }
            val train = metrics?.filterKeys { !it.startsWith("sys/") }.orEmpty()
            val done = (train.values.maxOfOrNull { it.step } ?: 0).toDouble()
            // A plain "steps" may be a sampler's, so one the run has passed is not its total.
            total("max_steps", "total_steps", "num_steps", "train_steps", "steps")?.takeIf { done <= it }?.let { return Progress(done, it, "step") }
            val epoch = train.entries.firstOrNull { it.key == "epoch" || it.key.endsWith("/epoch") }?.value?.last
            val epochs = total("num_train_epochs", "epochs", "max_epochs", "num_epochs")
            return if (epoch != null && epoch.isFinite() && epochs != null) Progress(epoch, epochs, "epoch") else null
        }
}

/** A machine's latest readings, from the sys/ metrics the client logs (sys/cpu, sys/ram_gb, sys/gpu{i}.util ...). */
data class SysLoad(val cpu: Double?, val ramGB: Double?, val gpus: List<Gpu>) {
    data class Gpu(val id: Int, val util: Double?, val memGB: Double?, val tempC: Double?, val powerW: Double?)

    companion object {
        fun of(metrics: Map<String, MetricSummary>?): SysLoad? {
            val v = metrics.orEmpty().filterKeys { it.startsWith("sys/") }.mapValues { it.value.last }
            if (v.isEmpty()) return null
            val ids = v.keys.mapNotNull { k ->
                if (k.startsWith("sys/gpu") && '.' in k) k.substring(7, k.indexOf('.')).toIntOrNull() else null
            }.toSortedSet()
            return SysLoad(v["sys/cpu"], v["sys/ram_gb"], ids.map { i ->
                Gpu(i, v["sys/gpu$i.util"], v["sys/gpu$i.mem_gb"], v["sys/gpu$i.temp_c"], v["sys/gpu$i.power_w"])
            })
        }
    }
}

/** One metric's history in buckets: the last value of each bucket, and its min and max. */
data class Series(val step: List<Int>, val min: List<Double>, val max: List<Double>, val last: List<Double>)

data class LogLine(val seq: Int, val ts: Long, val stream: Int, val line: String)

data class Media(val id: String, val key: String, val step: Int, val kind: String, val url: String, val caption: String?)

data class Alert(
    val id: Int, val run: String, val runName: String, val project: String, val ts: Long,
    val level: String, val title: String, val body: String, val dismissedAt: Long?,
)

/** A machine running `runyard beacon`: its latest reading, every process on its GPUs, and the runs on it. */
data class Machine(
    val name: String, val seenAt: Long, val silent: Boolean, val load: SysLoad,
    val procs: List<String>, val runs: List<String>,
)

// web/src/Kiosk.svelte's order and words: whatever needs a person comes first.
private val agentOrder = mapOf("waiting" to 0, "error" to 1, "done" to 2, "training" to 3, "working" to 3, "idle" to 4, "stale" to 5, "ended" to 6)
private val agentWords = mapOf(
    "waiting" to "Waiting for you", "error" to "Error", "done" to "Done", "training" to "Training", "working" to "Working",
    "idle" to "Idle", "stale" to "Quiet", "ended" to "Ended",
)

/** A coding agent session's card (docs/agents.md). */
data class Agent(
    val id: String, val tool: String, val label: String, val host: String, val cwd: String, val project: String?, val metric: String,
    val state: String, val said: String, val report: String, val runs: Int, val lastRun: LastRun?, val activeAt: Long,
) {
    data class LastRun(val id: String, val name: String)

    val title get() = label.ifEmpty { cwd.split('/').lastOrNull { it.isNotEmpty() } ?: tool }
    val word get() = agentWords[state] ?: state

    /** The project's main metric on this run; while the project names none, the run's own. */
    fun score(r: Run) = if (metric.isEmpty()) r.mainMetric else r.metrics?.get(metric)?.let { metric to it.last }
}

// ---- parsing ----

/** Go sends up to nine fraction digits and an offset; OffsetDateTime takes both. */
fun date(s: String?): Long? = s?.takeIf { it.isNotEmpty() }?.let { OffsetDateTime.parse(it).toInstant().toEpochMilli() }

/** A metric value: the server sends NaN and ±Infinity as strings, which JSON numbers cannot hold. */
fun num(v: Any?): Double = when (v) {
    is Number -> v.toDouble()
    is String -> v.toDoubleOrNull() ?: Double.NaN
    else -> Double.NaN
}

fun JSONObject.str(k: String): String? = if (isNull(k)) null else optString(k)
fun JSONObject.obj(k: String): JSONObject? = optJSONObject(k)
fun JSONObject.numOrNull(k: String): Double? = if (isNull(k)) null else num(opt(k))
private fun <T> JSONArray?.list(f: (JSONObject) -> T): List<T> =
    if (this == null) emptyList() else (0 until length()).map { f(getJSONObject(it)) }
private fun JSONArray?.nums(): List<Double> = if (this == null) emptyList() else (0 until length()).map { num(opt(it)) }

fun parseMe(s: String) = JSONObject(s).let { Me(it.optBoolean("auth"), it.str("user").orEmpty()) }

fun parseProjects(s: String) = JSONArray(s).list {
    Project(it.getString("name"), it.optInt("runs"), it.optInt("running"), date(it.str("last_run_at")))
}

fun parseRun(o: JSONObject) = Run(
    id = o.getString("id"),
    project = o.str("project").orEmpty(),
    name = o.str("name").orEmpty(),
    status = o.str("status").orEmpty(),
    config = o.obj("config"),
    summary = o.obj("summary"),
    tags = o.optJSONArray("tags")?.let { a -> (0 until a.length()).map { a.getString(it) } }.orEmpty(),
    notes = o.str("notes").orEmpty(),
    host = o.str("host").orEmpty(),
    git = o.obj("git"),
    startedAt = date(o.getString("started_at"))!!,
    endedAt = date(o.str("ended_at")),
    lastHeartbeatAt = date(o.str("last_heartbeat_at")),
    metrics = o.obj("metrics")?.let { m ->
        m.keys().asSequence().associateWith { k ->
            val x = m.getJSONObject(k)
            MetricSummary(x.optInt("count"), x.optInt("step"), num(x.opt("last")), num(x.opt("min")), num(x.opt("max")))
        }
    },
)

fun parseRuns(s: String) = JSONArray(s).list(::parseRun)

fun parseSeries(s: String): Map<String, Series> = JSONObject(s).let { o ->
    o.keys().asSequence().associateWith { k ->
        val x = o.getJSONObject(k)
        val steps = x.optJSONArray("step")
        Series((0 until (steps?.length() ?: 0)).map { steps!!.getInt(it) },
            x.optJSONArray("min").nums(), x.optJSONArray("max").nums(), x.optJSONArray("last").nums())
    }
}

fun parseLogs(s: String) = JSONArray(s).list {
    LogLine(it.getInt("seq"), date(it.getString("ts"))!!, it.optInt("stream", 1), it.optString("line"))
}

fun parseMedia(s: String) = JSONArray(s).list {
    Media(it.optString("id"), it.getString("key"), it.optInt("step"), it.optString("kind"), it.getString("url"),
        it.obj("meta")?.str("caption"))
}

fun parseAlerts(s: String) = JSONArray(s).list {
    Alert(it.getInt("id"), it.getString("run"), it.str("run_name").orEmpty(), it.str("project").orEmpty(),
        date(it.getString("ts"))!!, it.optString("level"), it.optString("title"), it.str("body").orEmpty(),
        date(it.str("dismissed_at")))
}

fun parseMachines(s: String) = JSONArray(s).list { m ->
    val st = m.optJSONObject("stats") ?: JSONObject()
    val gpus = st.optJSONArray("gpus")
    Machine(
        name = m.getString("name"),
        seenAt = date(m.str("seen_at")) ?: 0,
        silent = m.optBoolean("silent"),
        load = SysLoad(st.numOrNull("cpu"), st.numOrNull("ram_gb"), gpus.list {
            SysLoad.Gpu(it.optInt("i"), it.numOrNull("util"), it.numOrNull("mem_gb"), it.numOrNull("temp_c"), it.numOrNull("power_w"))
        }),
        procs = gpus.list { g ->
            g.optJSONArray("procs").list { p ->
                val name = p.str("name").orEmpty().ifEmpty { "pid ${p.optInt("pid")}" }
                // WSL's NVML gives no per-process memory: the beacon sends 0
                listOfNotNull("GPU ${g.optInt("i")}", name, p.str("user")?.ifEmpty { null }, num(p.opt("mem_gb")).takeIf { it > 0 }?.let { "%.1f GB".format(Locale.ROOT, it) })
                    .joinToString(" · ")
            }
        }.flatten(),
        runs = m.optJSONArray("runs").list { it.optString("name") },
    )
}

/** The cards, most urgent first, then the newest. */
fun parseAgents(s: String) = JSONArray(s).list {
    Agent(
        id = it.getString("id"), tool = it.str("tool").orEmpty(), label = it.str("label").orEmpty(), host = it.str("host").orEmpty(),
        cwd = it.str("cwd").orEmpty(), project = it.str("project"), metric = it.str("metric").orEmpty(), state = it.str("state").orEmpty(),
        said = it.str("said").orEmpty(), report = it.str("report").orEmpty(), runs = it.optInt("runs"),
        lastRun = it.obj("last_run")?.let { r -> Agent.LastRun(r.getString("id"), r.str("name").orEmpty()) },
        activeAt = date(it.getString("active_at"))!!,
    )
}.sortedWith(compareBy({ agentOrder[it.state] ?: agentOrder.size }, { -it.activeAt }))

/** Cards are extra, so this never fails: a server before 0.5.0 answers 404 and has none, any other failure keeps `last`. */
suspend fun loadAgents(get: suspend (String) -> String, last: List<Agent>): List<Agent> = try {
    parseAgents(get("/api/v1/agents?hours=24"))
} catch (e: Exception) {
    if ((e as? ApiError)?.status == 404) emptyList() else last
}

/** Nested objects as dotted keys, sorted, as the web UI's Config tab shows them. */
fun flat(o: JSONObject?, prefix: String = ""): List<Pair<String, String>> {
    if (o == null) return emptyList()
    return o.keys().asSequence().sorted().flatMap { k ->
        when (val v = o.opt(k)) {
            is JSONObject -> flat(v, "$prefix$k.").asSequence()
            else -> sequenceOf("$prefix$k" to text(v))
        }
    }.toList()
}

private fun text(v: Any?): String = when (v) {
    null, JSONObject.NULL -> "null"
    is Boolean -> v.toString()
    is Number -> fmt(v.toDouble())
    is JSONArray -> (0 until v.length()).joinToString(", ", "[", "]") { text(v.opt(it)) }
    is JSONObject -> "{…}"
    else -> v.toString()
}

/** Numbers short and the same in every locale, as C's %.4g: a dot for decimals, an exponent only when tiny or huge. */
fun fmt(v: Double): String {
    if (v.isNaN()) return "NaN"
    if (v.isInfinite()) return if (v > 0) "∞" else "-∞"
    if (v == floor(v) && abs(v) < 1e15) return v.toLong().toString()
    val r = BigDecimal(v).round(MathContext(4))
    val exp = floor(log10(abs(r.toDouble()))).toInt()
    if (exp < -4 || exp >= 4) {
        val mant = r.movePointLeft(exp).stripTrailingZeros().toPlainString()
        return "${mant}e${if (exp < 0) "-" else "+"}${"%02d".format(abs(exp))}"
    }
    return r.stripTrailingZeros().toPlainString()
}

/** Two units at most: 3d 4h, 2h 5m, 52m 10s. */
fun duration(seconds: Double): String {
    val s = maxOf(0L, seconds.toLong())
    val parts = listOf(s / 86400 to "d", s % 86400 / 3600 to "h", s % 3600 / 60 to "m", s % 60 to "s")
    val first = parts.indexOfFirst { it.first > 0 }.takeIf { it >= 0 } ?: return "0s"
    return parts.drop(first).take(2).filter { it.first > 0 }.joinToString(" ") { "${it.first}${it.second}" }
}

/** Encodes a value for a URL query. */
fun q(s: String): String = URLEncoder.encode(s, "UTF-8").replace("+", "%20")

// ---- talking to a server ----

class ApiError(val status: Int, override val message: String) : Exception(message)

/** The runyard API as the screens use it: a real server, or the demo inside the app. */
interface Backend {
    /** GET a server path (/api/v1/...) or a URL the server handed out, as text. */
    suspend fun get(path: String): String
    suspend fun send(path: String, method: String, json: String? = null): String
    /** An image's bytes (samples). */
    suspend fun bytes(url: String): ByteArray
}

class Http(val base: String, private val session: String?) : Backend {
    /** The status, the body and the Set-Cookie headers of one request. */
    fun call(path: String, method: String = "GET", json: String? = null, timeout: Int = 20_000): Triple<Int, ByteArray, List<String>> {
        val c = URL(if ("://" in path) path else base + path).openConnection() as HttpURLConnection
        try {
            c.requestMethod = method
            c.connectTimeout = timeout
            c.readTimeout = timeout
            c.instanceFollowRedirects = true
            if (session != null && "://" !in path) c.setRequestProperty("Cookie", "runyard_session=$session")
            if (json != null) {
                c.doOutput = true
                c.setRequestProperty("Content-Type", "application/json")
                c.outputStream.use { it.write(json.toByteArray()) }
            }
            val code = c.responseCode
            val body = (if (code in 200..299) c.inputStream else c.errorStream)?.use { it.readBytes() } ?: ByteArray(0)
            return Triple(code, body, c.headerFields["Set-Cookie"].orEmpty())
        } finally {
            c.disconnect()
        }
    }

    private suspend fun ok(path: String, method: String = "GET", json: String? = null): ByteArray = withContext(Dispatchers.IO) {
        val (code, body, _) = call(path, method, json)
        if (code !in 200..299) {
            val msg = runCatching { (JSONTokener(String(body)).nextValue() as JSONObject).getString("error") }.getOrNull()
            throw ApiError(code, msg ?: "HTTP $code")
        }
        body
    }

    override suspend fun get(path: String) = String(ok(path))
    override suspend fun send(path: String, method: String, json: String?) = String(ok(path, method, json))
    override suspend fun bytes(url: String) = ok(url)
}
