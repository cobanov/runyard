@file:OptIn(ExperimentalMaterial3Api::class, ExperimentalFoundationApi::class)

package dev.cobanov.runyard

import android.graphics.BitmapFactory
import android.text.format.DateUtils
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.SecondaryScrollableTabRow
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.material3.Tab
import androidx.compose.material3.TabRowDefaults
import androidx.compose.material3.TabRowDefaults.tabIndicatorOffset
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.FilterQuality
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.text.DateFormat
import java.util.Date
import kotlin.math.abs
import kotlin.math.roundToInt

// ---- small parts ----

val Mono = TextStyle(fontFamily = FontFamily.Monospace)
val Num = TextStyle(fontFeatureSettings = "tnum")

/** Runs load now and then again every `every` ms while the screen is shown. */
@Composable
fun Poll(key: Any?, every: () -> Long, load: suspend () -> Unit) {
    val l by rememberUpdatedState(load)
    val e by rememberUpdatedState(every)
    LaunchedEffect(key) {
        while (true) {
            l()
            delay(e())
        }
    }
}

@Composable
fun Screen(title: String, back: (() -> Unit)? = null, actions: @Composable () -> Unit = {}, content: @Composable () -> Unit) {
    Column(Modifier.fillMaxSize()) {
        TopAppBar(
            title = { Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis, fontWeight = FontWeight.SemiBold) },
            navigationIcon = { if (back != null) IconButton(onClick = back) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") } },
            actions = { actions() },
            windowInsets = WindowInsets(0),
            colors = TopAppBarDefaults.topAppBarColors(containerColor = T.paper, scrolledContainerColor = T.paper),
        )
        content()
    }
}

@Composable
fun Refreshable(load: suspend () -> Unit, content: @Composable () -> Unit) {
    var refreshing by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    PullToRefreshBox(refreshing, onRefresh = { scope.launch { refreshing = true; load(); refreshing = false } }, Modifier.fillMaxSize()) { content() }
}

@Composable
fun Panel(modifier: Modifier = Modifier, padding: Int = 12, content: @Composable ColumnScope.() -> Unit) {
    Column(
        modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).background(T.panel).border(1.dp, T.rule, RoundedCornerShape(8.dp)).padding(padding.dp),
        content = content,
    )
}

@Composable
fun SectionTitle(text: String) = Text(text, fontSize = 13.sp, color = T.muted, modifier = Modifier.padding(top = 4.dp))

@Composable
fun Dot(color: Color, size: Int = 8) = Box(Modifier.size(size.dp).clip(CircleShape).background(color))

@Composable
fun KeyValue(key: String, value: String) {
    Row(Modifier.fillMaxWidth().padding(vertical = 7.dp), verticalAlignment = Alignment.Top) {
        Text(key, fontSize = 14.sp, color = T.muted, modifier = Modifier.padding(end = 12.dp))
        Spacer(Modifier.weight(1f))
        SelectionContainer { Text(value, fontSize = 14.sp, style = Num, textAlign = TextAlign.End) }
    }
}

@Composable
fun Empty(title: String, text: String) {
    Column(Modifier.fillMaxWidth().padding(vertical = 40.dp, horizontal = 24.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        Text(title, fontWeight = FontWeight.SemiBold, fontSize = 17.sp)
        Text(text, color = T.muted, fontSize = 14.sp, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 6.dp))
    }
}

fun ago(t: Long): String = DateUtils.getRelativeTimeSpanString(t, System.currentTimeMillis(), DateUtils.MINUTE_IN_MILLIS).toString()
fun clock(t: Long): String = DateFormat.getTimeInstance(DateFormat.SHORT).format(Date(t))
fun plural(n: Int, word: String) = if (n == 1) "1 $word" else "$n ${word}s"

@Composable
fun DeleteDialog(title: String, text: String, confirm: String, onDismiss: () -> Unit, onConfirm: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(title) }, text = { Text(text) },
        confirmButton = { TextButton(onClick = { onDismiss(); onConfirm() }) { Text(confirm, color = T.failed) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel", color = T.ink) } },
        containerColor = T.panel, shape = RoundedCornerShape(8.dp),
    )
}

@Composable
fun Failure(text: String?, onDismiss: () -> Unit) {
    if (text != null) AlertDialog(
        onDismissRequest = onDismiss, title = { Text("Can't delete") }, text = { Text(text) },
        confirmButton = { TextButton(onClick = onDismiss) { Text("OK", color = T.ink) } }, containerColor = T.panel, shape = RoundedCornerShape(8.dp),
    )
}

// ---- home ----

/** Home: what is training now, each at a glance, then every project. */
@Composable
fun HomeScreen(store: Store, open: (Route) -> Unit) {
    var running by remember { mutableStateOf(listOf<Run>()) }
    var spark by remember { mutableStateOf(mapOf<String, List<Double>>()) }
    var projects by remember { mutableStateOf(listOf<Project>()) }
    var loaded by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var deleting by remember { mutableStateOf<Project?>(null) }
    var failure by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val load: suspend () -> Unit = {
        try {
            val runs = parseRuns(store.get("/api/v1/runs?status=running&limit=50&metrics=1"))
            val ps = parseProjects(store.get("/api/v1/projects"))
            val sp = runs.mapNotNull { r ->
                val k = r.mainMetric?.first ?: return@mapNotNull null
                runCatching { r.id to parseSeries(store.get("/api/v1/runs/${r.id}/metrics?points=40&keys=${q(k)}"))[k]!!.last.filter { it.isFinite() } }.getOrNull()
            }.toMap()
            running = runs; spark = sp; projects = ps
            loaded = true; error = null
        } catch (e: Exception) {
            error = e.message
        }
    }
    Poll(Unit, { if (running.isEmpty()) 10_000 else 5_000 }, load)
    Screen("Projects") {
        Refreshable(load) {
            LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxSize()) {
                if (running.isNotEmpty()) {
                    item { SectionTitle("Running") }
                    items(running, key = { it.id }) { r -> LiveRunCard(r, spark[r.id].orEmpty()) { open(Route.RunR(r.id)) } }
                }
                item { SectionTitle("All projects") }
                if (projects.isNotEmpty()) item {
                    Panel(padding = 0) {
                        projects.forEachIndexed { i, p ->
                            if (i > 0) HorizontalDivider(color = T.rule)
                            ProjectRow(p, { open(Route.Project(p.name)) }) { deleting = p }
                        }
                    }
                }
                if (loaded && projects.isEmpty()) item { Empty("No projects yet", "Runs show up here once a training script calls runyard.init().") }
                else if (!loaded && error != null) item { Empty("Can't load projects", error!!) }
                else if (!loaded) item { Box(Modifier.fillMaxWidth().padding(40.dp), Alignment.Center) { CircularProgressIndicator(color = T.running) } }
            }
        }
    }
    deleting?.let { p ->
        DeleteDialog("Delete ${p.name}?", "Its ${plural(p.runs, "run")}, with their metrics, logs, samples and artifacts, are deleted for good.",
            "Delete project", { deleting = null }) {
            scope.launch {
                try { store.delete("/api/v1/projects/${q(p.name)}"); load() } catch (e: Exception) { failure = e.message }
            }
        }
    }
    Failure(failure) { failure = null }
}

/** A running run on the home screen: enough to see how it is going without opening it. */
@Composable
fun LiveRunCard(run: Run, spark: List<Double>, onClick: () -> Unit) {
    Panel(Modifier.combinedClickable(onClick = onClick)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Dot(T.status(run.status))
            Text(run.name, fontWeight = FontWeight.SemiBold, fontSize = 16.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(start = 8.dp).weight(1f))
            Text(run.project, fontSize = 12.sp, color = T.muted, maxLines = 1, modifier = Modifier.padding(start = 8.dp))
        }
        run.mainMetric?.let { (key, value) ->
            Row(Modifier.padding(top = 10.dp), verticalAlignment = Alignment.Bottom) {
                Column(Modifier.weight(1f)) {
                    Text(fmt(value), fontSize = 24.sp, fontWeight = FontWeight.Bold, style = Num)
                    Text("$key · step ${run.metrics?.get(key)?.step ?: 0}", fontSize = 12.sp, color = T.muted, maxLines = 1)
                }
                if (spark.size > 1) Sparkline(spark, Modifier.width(110.dp).height(34.dp))
            }
        }
        Text(listOfNotNull(duration(run.duration()), run.lastHeartbeatAt?.let { "heartbeat ${duration((System.currentTimeMillis() - it) / 1000.0)} ago" },
            run.host.ifEmpty { null }?.let { "on $it" }).joinToString(" · "),
            fontSize = 12.sp, color = T.muted, maxLines = 1, modifier = Modifier.padding(top = 8.dp))
        SysLoad.of(run.metrics)?.let { Spacer(Modifier.height(8.dp)); MachineStrip(it) }
    }
}

/** The machine at a glance: a bar for the CPU and for each GPU. */
@Composable
fun MachineStrip(load: SysLoad, dim: Boolean = false) {
    Column(
        Modifier.fillMaxWidth().alpha(if (dim) 0.5f else 1f).clip(RoundedCornerShape(6.dp)).background(T.soft).padding(10.dp),
        verticalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        load.cpu?.let { LoadRow("CPU", it, load.ramGB?.let { r -> "RAM ${r.roundToInt()} GB" } ?: "") }
        load.gpus.forEach { g ->
            LoadRow("GPU ${g.id}", g.util, listOfNotNull(g.memGB?.let { "${it.roundToInt()} GB" }, g.tempC?.let { "${it.roundToInt()}°" }).joinToString(" · "))
        }
    }
}

@Composable
private fun LoadRow(name: String, util: Double?, details: String) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(name, fontSize = 12.sp, modifier = Modifier.width(46.dp))
        Meter(util ?: 0.0, Modifier.weight(1f))
        Text(util?.let { "${it.roundToInt()}%" } ?: "–", fontSize = 12.sp, fontWeight = FontWeight.SemiBold, style = Num, textAlign = TextAlign.End, modifier = Modifier.width(40.dp))
        Text(details, fontSize = 12.sp, color = T.muted, maxLines = 1, style = Num, modifier = Modifier.padding(start = 8.dp).width(84.dp))
    }
}

@Composable
fun Meter(percent: Double, modifier: Modifier = Modifier) {
    LinearProgressIndicator(
        progress = { (percent / 100).toFloat().coerceIn(0f, 1f) }, modifier = modifier.height(5.dp).clip(RoundedCornerShape(3.dp)),
        color = if (percent > 90) T.crashed else T.running, trackColor = T.rule, gapSize = 0.dp, drawStopIndicator = {},
    )
}

@Composable
fun Sparkline(values: List<Double>, modifier: Modifier) {
    val color = T.running
    Canvas(modifier) {
        val lo = values.min()
        val hi = values.max()
        val span = if (hi > lo) hi - lo else 1.0
        val path = Path()
        values.forEachIndexed { i, v ->
            val x = size.width * i / (values.size - 1)
            val y = (size.height * (1 - (v - lo) / span)).toFloat()
            if (i == 0) path.moveTo(x, y) else path.lineTo(x, y)
        }
        drawPath(path, color, style = Stroke(1.5.dp.toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round))
    }
}

@Composable
fun ProjectRow(p: Project, onClick: () -> Unit, onLongClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().combinedClickable(onClick = onClick, onLongClick = onLongClick).padding(horizontal = 12.dp, vertical = 11.dp),
        verticalAlignment = Alignment.CenterVertically) {
        Dot(if (p.running > 0) T.running else T.finished)
        Column(Modifier.padding(start = 10.dp).weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(p.name, fontWeight = FontWeight.SemiBold, fontSize = 16.sp)
                if (p.running > 0) Text("${p.running} running", fontSize = 12.sp, color = T.running, fontWeight = FontWeight.Medium,
                    modifier = Modifier.padding(start = 8.dp).border(1.dp, T.rule, RoundedCornerShape(6.dp)).padding(horizontal = 6.dp, vertical = 1.dp))
            }
            Text(listOfNotNull(plural(p.runs, "run"), p.lastRunAt?.let { "last " + ago(it) }).joinToString(" · "), fontSize = 13.sp, color = T.muted)
        }
    }
}

// ---- a project's runs ----

/** One project's runs, newest first. Long-press to delete one. */
@Composable
fun RunsScreen(store: Store, project: String, open: (Route) -> Unit, back: () -> Unit) {
    var runs by remember { mutableStateOf(listOf<Run>()) }
    var loaded by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var deleting by remember { mutableStateOf<Run?>(null) }
    var failure by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val load: suspend () -> Unit = {
        try {
            runs = parseRuns(store.get("/api/v1/runs?project=${q(project)}&limit=200&metrics=1"))
            loaded = true; error = null
        } catch (e: Exception) { error = e.message }
    }
    Poll(project, { if (runs.any { it.running }) 10_000 else 30_000 }, load)
    Screen(project, back) {
        Refreshable(load) {
            LazyColumn(contentPadding = PaddingValues(16.dp), modifier = Modifier.fillMaxSize()) {
                if (runs.isNotEmpty()) item {
                    Panel(padding = 0) {
                        runs.forEachIndexed { i, r ->
                            if (i > 0) HorizontalDivider(color = T.rule)
                            RunRow(r, { open(Route.RunR(r.id)) }) { deleting = r }
                        }
                    }
                }
                if (loaded && runs.isEmpty()) item { Empty("No runs", "") }
                else if (!loaded && error != null) item { Empty("Can't load runs", error!!) }
            }
        }
    }
    deleting?.let { r ->
        DeleteDialog("Delete ${r.name}?", "Its metrics, logs, samples and artifacts are deleted for good." +
            if (r.running) " It is still running; what it sends from now on is refused." else "", "Delete run", { deleting = null }) {
            scope.launch {
                try { store.delete("/api/v1/runs/${r.id}"); load() } catch (e: Exception) { failure = e.message }
            }
        }
    }
    Failure(failure) { failure = null }
}

@Composable
fun RunRow(run: Run, onClick: () -> Unit, onLongClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().combinedClickable(onClick = onClick, onLongClick = onLongClick).padding(horizontal = 12.dp, vertical = 11.dp),
        verticalAlignment = Alignment.CenterVertically) {
        Dot(T.status(run.status), 9)
        Column(Modifier.padding(start = 10.dp).weight(1f)) {
            Text(run.name, fontWeight = FontWeight.SemiBold, fontSize = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(listOfNotNull(run.status, duration(run.duration()), if (run.running) null else ago(run.startedAt)).joinToString(" · "),
                fontSize = 13.sp, color = T.muted, maxLines = 1)
            if (run.tags.isNotEmpty()) Text(run.tags.joinToString("  "), fontSize = 12.sp, color = T.muted, maxLines = 1)
        }
        run.mainMetric?.let { (k, v) ->
            Column(horizontalAlignment = Alignment.End, modifier = Modifier.padding(start = 8.dp)) {
                Text(fmt(v), fontWeight = FontWeight.SemiBold, style = Num, color = if (v.isFinite()) T.ink else T.failed)
                Text(k, fontSize = 11.sp, color = T.muted, maxLines = 1)
            }
        }
    }
}

// ---- alerts ----

@Composable
fun AlertsScreen(store: Store, open: (Route) -> Unit) {
    var alerts by remember { mutableStateOf(listOf<Alert>()) }
    var loaded by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val load: suspend () -> Unit = {
        try {
            alerts = parseAlerts(store.get("/api/v1/alerts?limit=100")).sortedByDescending { it.ts }
            loaded = true; error = null
        } catch (e: Exception) { error = e.message }
    }
    Poll(Unit, { 15_000 }, load)
    Screen("Alerts", actions = {
        if (alerts.any { it.dismissedAt == null }) TextButton(onClick = {
            scope.launch {
                runCatching { store.api.send("/api/v1/alerts/dismiss", "POST", JSONObject().put("ids", JSONArray()).toString()) }
                    .onFailure { error = it.message }
                load()
            }
        }) { Text("Dismiss all", color = T.ink) }
    }) {
        Refreshable(load) {
            LazyColumn(contentPadding = PaddingValues(16.dp), modifier = Modifier.fillMaxSize()) {
                if (alerts.isNotEmpty()) item {
                    Panel(padding = 0) {
                        alerts.forEachIndexed { i, a ->
                            if (i > 0) HorizontalDivider(color = T.rule)
                            Box(Modifier.combinedClickable { open(Route.RunR(a.run)) }.alpha(if (a.dismissedAt == null) 1f else 0.5f).padding(12.dp)) {
                                AlertRow(a, showRun = true)
                            }
                        }
                    }
                }
                if (loaded && alerts.isEmpty()) item { Empty("No alerts", "Alerts from run.alert() and the server's checks (NaN loss, stalls) land here.") }
                else if (!loaded && error != null) item { Empty("Can't load alerts", error!!) }
            }
        }
    }
}

@Composable
fun AlertRow(alert: Alert, showRun: Boolean = false) {
    Row(verticalAlignment = Alignment.Top) {
        Box(Modifier.padding(top = 6.dp)) { Dot(T.level(alert.level)) }
        Column(Modifier.padding(start = 10.dp)) {
            Text(alert.title, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
            alert.body.lineSequence().firstOrNull()?.takeIf { it.isNotEmpty() }?.let { Text(it, fontSize = 14.sp, color = T.muted, maxLines = 2) }
            Text(listOfNotNull(if (showRun) "${alert.project}/${alert.runName}" else null, ago(alert.ts)).joinToString(" · "), fontSize = 12.sp, color = T.muted)
        }
    }
}

// ---- a run ----

/** Three significant digits for axis labels. */
private fun axis(v: Double) = if (v.isFinite() && v != 0.0) java.math.BigDecimal(v).round(java.math.MathContext(3)).toDouble() else v

private val tabs = listOf("Charts", "System", "Samples", "Logs", "Config")

/** One run, with the web UI's tabs. */
@Composable
fun RunScreen(store: Store, id: String, back: () -> Unit) {
    var run by remember { mutableStateOf<Run?>(null) }
    var series by remember { mutableStateOf(mapOf<String, Series>()) }
    var alerts by remember { mutableStateOf(listOf<Alert>()) }
    var error by remember { mutableStateOf<String?>(null) }
    var tab by remember { mutableStateOf("Charts") }
    val load: suspend () -> Unit = {
        try {
            run = parseRun(JSONObject(store.get("/api/v1/runs/$id")))
            series = parseSeries(store.get("/api/v1/runs/$id/metrics?points=300"))
            alerts = parseAlerts(store.get("/api/v1/alerts?run=$id&limit=10"))
            error = null
        } catch (e: Exception) { error = e.message }
    }
    Poll(id, { if (run?.running == false) 60_000 else 5_000 }, load)
    val trainKeys = series.keys.filter { !it.startsWith("sys/") }.sorted()
    val sysKeys = series.keys.filter { it.startsWith("sys/") }.sorted()
    val shown = tabs.filter { it != "System" || sysKeys.isNotEmpty() }
    Screen(run?.name ?: "Run", back) {
        val r = run
        if (r == null) {
            if (error != null) Empty("Can't load this run", error!!)
            else Box(Modifier.fillMaxSize(), Alignment.Center) { CircularProgressIndicator(color = T.running) }
            return@Screen
        }
        Refreshable(load) {
            LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.fillMaxSize()) {
                item { RunHeader(r) }
                if (alerts.isNotEmpty()) item {
                    Panel { alerts.forEachIndexed { i, a -> if (i > 0) Spacer(Modifier.height(10.dp)); AlertRow(a) } }
                }
                item {
                    val selected = shown.indexOf(tab).coerceAtLeast(0)
                    SecondaryScrollableTabRow(
                        selectedTabIndex = selected, containerColor = T.paper, contentColor = T.ink, edgePadding = 0.dp,
                        divider = { HorizontalDivider(color = T.rule) },
                        indicator = { TabRowDefaults.SecondaryIndicator(Modifier.tabIndicatorOffset(selected), height = 2.dp, color = T.ink) },
                    ) {
                        shown.forEach { t ->
                            Tab(selected = t == tab, onClick = { tab = t }, text = { Text(t, fontSize = 14.sp) }, selectedContentColor = T.ink, unselectedContentColor = T.muted)
                        }
                    }
                }
                when (tab) {
                    "Charts" -> if (trainKeys.isEmpty()) item { Empty("No metrics yet", "They appear as soon as the script calls run.log().") }
                        else items(trainKeys, key = { it }) { k -> MetricChart(k, series.getValue(k), "step") }
                    "System" -> {
                        SysLoad.of(r.metrics)?.let { item { SystemNow(it) } }
                        items(sysKeys, key = { it }) { k -> MetricChart(k.removePrefix("sys/"), series.getValue(k), "second") }
                    }
                    "Samples" -> item { SamplesTab(store, id, r.running) }
                    "Logs" -> item { LogsTab(store, id, r.running) }
                    else -> item { ConfigTab(r) }
                }
            }
        }
    }
}

@Composable
fun RunHeader(run: Run) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(run.project, fontSize = 14.sp, color = T.muted)
        Text(run.name, fontSize = 22.sp, fontWeight = FontWeight.Bold)
        Row(verticalAlignment = Alignment.CenterVertically) {
            Dot(T.status(run.status))
            Text(run.status, color = T.status(run.status), fontWeight = FontWeight.SemiBold, fontSize = 14.sp, modifier = Modifier.padding(start = 6.dp, end = 8.dp))
            Text("for ${duration(run.duration())}, started ${ago(run.startedAt)}", fontSize = 14.sp, color = T.muted)
        }
        val beat = run.lastHeartbeatAt
        if (run.running && beat != null) Text("Last heartbeat ${duration((System.currentTimeMillis() - beat) / 1000.0)} ago" +
            run.host.ifEmpty { null }?.let { " from $it" }.orEmpty(), fontSize = 12.sp, color = T.muted)
        else if (run.host.isNotEmpty()) Text("on ${run.host}", fontSize = 12.sp, color = T.muted)
        if (run.notes.isNotEmpty()) Text(run.notes, fontSize = 14.sp, modifier = Modifier.padding(top = 4.dp))
    }
}

/** A metric's line, with the min–max band of each bucket when the server thinned it out. Touch shows a value. */
@Composable
fun MetricChart(title: String, s: Series, xLabel: String) {
    data class P(val step: Int, val last: Double, val min: Double, val max: Double)
    val pts = remember(s) { s.step.indices.map { P(s.step[it], s.last[it], s.min.getOrElse(it) { s.last[it] }, s.max.getOrElse(it) { s.last[it] }) }.filter { it.last.isFinite() } }
    val band = pts.any { it.min != it.max && it.min.isFinite() && it.max.isFinite() }
    var touch by remember { mutableStateOf<Float?>(null) }
    var width by remember { mutableStateOf(1f) }
    val lo0 = pts.minOfOrNull { if (band && it.min.isFinite()) minOf(it.min, it.last) else it.last } ?: 0.0
    val hi0 = pts.maxOfOrNull { if (band && it.max.isFinite()) maxOf(it.max, it.last) else it.last } ?: 1.0
    val pad = if (hi0 > lo0) (hi0 - lo0) * 0.06 else 0.5 // the line never touches the edges
    val lo = lo0 - pad
    val hi = hi0 + pad
    val x0 = pts.firstOrNull()?.step ?: 0
    val x1 = pts.lastOrNull()?.step ?: 1
    var left by remember { mutableStateOf(0f) }
    val pick = touch?.let { tx -> val step = x0 + (x1 - x0) * ((tx - left) / (width - left)).coerceIn(0f, 1f); pts.minByOrNull { abs(it.step - step) } }
    val t = T
    val measurer = rememberTextMeasurer()
    Panel {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(title, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            (pick ?: pts.lastOrNull())?.let { Text("${fmt(it.last)}  $xLabel ${it.step}", fontSize = 12.sp, color = t.muted, style = Num) }
        }
        Spacer(Modifier.height(8.dp))
        Canvas(Modifier.fillMaxWidth().height(150.dp).pointerInput(pts) {
            awaitEachGesture {
                val down = awaitFirstDown()
                touch = down.position.x
                while (true) {
                    val e = awaitPointerEvent()
                    val c = e.changes.firstOrNull() ?: break
                    if (!c.pressed) break
                    touch = c.position.x
                }
                touch = null
            }
        }) {
            width = size.width
            val span = hi - lo
            val labels = (0..3).map { fmt(axis(hi - span * it / 3)) }
            val style = TextStyle(fontSize = 10.sp, color = t.muted)
            val labelW = labels.maxOf { measurer.measure(it, style).size.width } + 8.dp.toPx()
            left = labelW
            fun x(step: Int) = labelW + (size.width - labelW) * (if (x1 > x0) (step - x0).toFloat() / (x1 - x0) else 0.5f)
            fun y(v: Double) = (size.height * (1 - (v - lo) / span)).toFloat()
            for (i in 0..3) {
                val gy = size.height * i / 3f
                drawLine(t.rule, Offset(labelW, gy), Offset(size.width, gy), 1f)
                val h = measurer.measure(labels[i], style).size.height
                drawText(measurer, labels[i], Offset(0f, (gy - h / 2f).coerceIn(0f, size.height - h)), style)
            }
            if (pts.isEmpty()) return@Canvas
            if (band) {
                val area = Path()
                pts.forEachIndexed { i, p -> if (i == 0) area.moveTo(x(p.step), y(p.max)) else area.lineTo(x(p.step), y(p.max)) }
                pts.asReversed().forEach { p -> area.lineTo(x(p.step), y(p.min)) }
                area.close()
                drawPath(area, t.running.copy(alpha = 0.15f))
            }
            val line = Path()
            pts.forEachIndexed { i, p -> if (i == 0) line.moveTo(x(p.step), y(p.last)) else line.lineTo(x(p.step), y(p.last)) }
            drawPath(line, t.running, style = Stroke(1.5.dp.toPx(), join = StrokeJoin.Round))
            pick?.let {
                drawLine(t.muted.copy(alpha = 0.5f), Offset(x(it.step), 0f), Offset(x(it.step), size.height), 1f)
                drawCircle(t.running, 4.dp.toPx(), Offset(x(it.step), y(it.last)))
            }
        }
        if (pts.size < s.step.size) Text("${s.step.size - pts.size} NaN or infinite values not drawn", fontSize = 11.sp, color = t.crashed, modifier = Modifier.padding(top = 4.dp))
    }
}

/** The latest readings of the SDK's system monitor: the machine's load at a glance, one row per GPU. */
@Composable
fun SystemNow(load: SysLoad) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            load.cpu?.let { Tile("CPU", "${it.roundToInt()}%", it, Modifier.weight(1f)) }
            load.ramGB?.let { Tile("RAM", "%.1f GB".format(it), null, Modifier.weight(1f)) }
        }
        load.gpus.forEach { g ->
            Panel {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("GPU ${g.id}", fontWeight = FontWeight.SemiBold, fontSize = 14.sp, modifier = Modifier.weight(1f))
                    g.util?.let { Text("${it.roundToInt()}%", fontWeight = FontWeight.Bold, fontSize = 18.sp, style = Num) }
                }
                g.util?.let { Meter(it, Modifier.fillMaxWidth().padding(vertical = 6.dp)) }
                Text(listOfNotNull(g.memGB?.let { "%.1f GB".format(it) }, g.tempC?.let { "${it.roundToInt()} °C" }, g.powerW?.let { "${it.roundToInt()} W" }).joinToString(" · "),
                    fontSize = 12.sp, color = T.muted, style = Num)
            }
        }
    }
}

@Composable
private fun Tile(title: String, value: String, load: Double?, modifier: Modifier) {
    Panel(modifier) {
        Text(title, fontSize = 12.sp, color = T.muted)
        Text(value, fontWeight = FontWeight.Bold, fontSize = 18.sp, style = Num)
        load?.let { Meter(it, Modifier.fillMaxWidth().padding(top = 4.dp)) }
    }
}

@Composable
fun ConfigTab(run: Run) {
    Column(verticalArrangement = Arrangement.spacedBy(20.dp)) {
        flat(run.summary).takeIf { it.isNotEmpty() }?.let { Table("Summary", it) }
        Table("Config", flat(run.config))
        Table("Run", listOf("id" to run.id, "host" to run.host, "tags" to run.tags.joinToString(", ")) + flat(run.git, "git."))
    }
}

@Composable
private fun Table(title: String, rows: List<Pair<String, String>>) {
    Column {
        Text(title, fontWeight = FontWeight.SemiBold, fontSize = 16.sp, modifier = Modifier.padding(bottom = 6.dp))
        if (rows.isEmpty()) Text("Nothing recorded.", fontSize = 14.sp, color = T.muted)
        rows.forEachIndexed { i, (k, v) ->
            if (i > 0) HorizontalDivider(color = T.rule)
            KeyValue(k, v)
        }
    }
}

/** stdout and stderr, newest at the bottom; follows the run while it is running. */
@Composable
fun LogsTab(store: Store, id: String, running: Boolean) {
    var lines by remember { mutableStateOf(listOf<LogLine>()) }
    var loaded by remember { mutableStateOf(false) }
    val state = rememberLazyListState()
    Poll(id, { if (running) 3_000 else 60_000 }) {
        val path = lines.lastOrNull()?.let { "/api/v1/runs/$id/logs?after=${it.seq}&limit=2000" } ?: "/api/v1/runs/$id/logs?tail&limit=1000"
        runCatching { parseLogs(store.get(path)) }.getOrNull()?.let { next ->
            val atEnd = !state.canScrollForward
            lines = (lines + next).takeLast(3000)
            loaded = true
            if (next.isNotEmpty() && atEnd) state.scrollToItem(lines.lastIndex)
        }
    }
    Box(Modifier.fillMaxWidth().height(520.dp).clip(RoundedCornerShape(8.dp)).background(T.panel).border(1.dp, T.rule, RoundedCornerShape(8.dp))) {
        if (loaded && lines.isEmpty()) Text("No output yet.", color = T.muted, modifier = Modifier.align(Alignment.Center))
        SelectionContainer {
            LazyColumn(state = state, contentPadding = PaddingValues(10.dp)) {
                items(lines, key = { it.seq }) { l ->
                    Text(l.line, fontSize = 11.sp, lineHeight = 15.sp, style = Mono, color = if (l.stream == 2) T.failed else T.ink)
                }
            }
        }
    }
}

/** Images and text tables by key, with a slider through the steps; the latest step by default. */
@Composable
fun SamplesTab(store: Store, id: String, running: Boolean) {
    var media by remember { mutableStateOf(listOf<Media>()) }
    var loaded by remember { mutableStateOf(false) }
    val picked = remember { mutableStateMapOf<String, Int>() }
    Poll(id, { if (running) 10_000 else 120_000 }) {
        runCatching { media = parseMedia(store.get("/api/v1/runs/$id/media")) }
        loaded = true
    }
    val groups = media.groupBy { it.key }
    Column(verticalArrangement = Arrangement.spacedBy(20.dp)) {
        if (loaded && media.isEmpty()) Empty("No samples yet", "run.image(), run.video() and run.text() show up here.")
        groups.keys.sorted().forEach { key ->
            val items = groups.getValue(key)
            val steps = items.map { it.step }.distinct().sorted()
            val index = minOf(picked[key] ?: steps.lastIndex, steps.lastIndex)
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(key, fontWeight = FontWeight.SemiBold, fontSize = 16.sp, modifier = Modifier.weight(1f))
                    Text("step ${steps[index]}", fontSize = 12.sp, color = T.muted, style = Num)
                }
                if (steps.size > 1) Slider(
                    value = index.toFloat(), onValueChange = { picked[key] = it.roundToInt() }, valueRange = 0f..steps.lastIndex.toFloat(),
                    steps = (steps.size - 2).coerceAtLeast(0),
                    colors = SliderDefaults.colors(thumbColor = T.ink, activeTrackColor = T.ink, inactiveTrackColor = T.rule, activeTickColor = Color.Transparent, inactiveTickColor = Color.Transparent),
                )
                val now = items.filter { it.step == steps[index] }
                val images = now.filter { it.kind == "image" || it.url.endsWith(".gif") }
                if (images.isNotEmpty()) Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    images.take(4).forEach { m ->
                        Column(Modifier.weight(1f)) {
                            RemoteImage(store, m.url)
                            m.caption?.let { Text(it, fontSize = 11.sp, color = T.muted, maxLines = 2) }
                        }
                    }
                }
                now.filter { it.kind == "text" }.forEach { TextTable(store, it.url) }
                now.filter { it.kind == "video" && !it.url.endsWith(".gif") }.forEach {
                    Text("Video at step ${it.step}: open it on the web UI.", fontSize = 13.sp, color = T.muted)
                }
            }
        }
    }
}

/** An image fetched with the session. Pixel art stays sharp. */
@Composable
fun RemoteImage(store: Store, url: String) {
    var image by remember(url) { mutableStateOf<ImageBitmap?>(null) }
    LaunchedEffect(url) {
        image = runCatching {
            val b = store.api.bytes(url)
            withContext(kotlinx.coroutines.Dispatchers.Default) { BitmapFactory.decodeByteArray(b, 0, b.size)?.asImageBitmap() }
        }.getOrNull()
    }
    val img = image
    if (img == null) Box(Modifier.fillMaxWidth().aspectRatio(1f).clip(RoundedCornerShape(8.dp)).background(T.soft))
    else Image(img, null, Modifier.fillMaxWidth().aspectRatio(img.width.toFloat() / img.height).clip(RoundedCornerShape(8.dp)),
        contentScale = ContentScale.Fit, filterQuality = if (img.width < 128) FilterQuality.None else FilterQuality.Medium)
}

/** run.text(): a JSON list of rows, each shown as key–value lines. */
@Composable
fun TextTable(store: Store, url: String) {
    var rows by remember(url) { mutableStateOf(listOf<JSONObject>()) }
    LaunchedEffect(url) {
        rows = runCatching { JSONArray(store.get(url)).let { a -> (0 until a.length()).map { a.getJSONObject(it) } } }.getOrDefault(emptyList())
    }
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        rows.forEach { row ->
            Panel {
                row.keys().asSequence().sorted().forEach { k ->
                    Text(k, fontSize = 12.sp, color = T.muted)
                    Text(row.opt(k).toString(), fontSize = 14.sp, modifier = Modifier.padding(bottom = 4.dp))
                }
            }
        }
    }
}
