@file:OptIn(ExperimentalMaterial3Api::class, ExperimentalFoundationApi::class)

package dev.cobanov.runyard

import android.graphics.Bitmap
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
import androidx.compose.foundation.gestures.calculatePan
import androidx.compose.foundation.gestures.calculateZoom
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
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
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
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.compositionLocalOf
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
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.FilterQuality
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
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
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.text.DateFormat
import java.util.Date
import java.util.Locale
import kotlin.math.abs
import kotlin.math.roundToInt

// ---- small parts ----

val Mono = TextStyle(fontFamily = FontFamily.Monospace)
val Num = TextStyle(fontFeatureSettings = "tnum")

/** False for a screen under the one on top: it stays composed, so its list keeps its place, but it does not poll. */
val LocalShown = compositionLocalOf { true }

/** Runs load now and then again every `every` ms while the screen is on top and the app is in front. */
@Composable
fun Poll(key: Any?, every: () -> Long, load: suspend () -> Unit) {
    val l by rememberUpdatedState(load)
    val e by rememberUpdatedState(every)
    val owner = LocalLifecycleOwner.current
    val shown = LocalShown.current
    LaunchedEffect(key, owner, shown) {
        if (shown) owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                l()
                delay(e())
            }
        }
    }
}

/** What a failed refresh leaves in `error`; a cancelled one (the screen went away) is not a failure. */
fun failure(e: Exception): String = if (e is CancellationException) throw e else if (e is ApiError) e.message else "Can't reach the server."

/** One quiet line while refreshes fail: what is on the screen is as old as it says. */
@Composable
fun Stale(since: Long, error: String) {
    var now by remember { mutableStateOf(System.currentTimeMillis()) }
    LaunchedEffect(Unit) { while (true) { delay(1000); now = System.currentTimeMillis() } }
    Text("Not updated for ${duration((now - since) / 1000.0)}. $error", fontSize = 13.sp, color = T.crashed, style = Num)
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

/** Home: the coding agents, then what is training now, each at a glance, the runs that just ended, then every project. */
@Composable
fun HomeScreen(store: Store, open: (Route) -> Unit) {
    var runs by remember { mutableStateOf(listOf<Run>()) }
    var okAt by remember { mutableStateOf(0L) }
    var spark by remember { mutableStateOf(mapOf<String, List<Double>>()) }
    var projects by remember { mutableStateOf(listOf<Project>()) }
    var agents by remember { mutableStateOf(listOf<Agent>()) }
    var loaded by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var deleting by remember { mutableStateOf<Project?>(null) }
    var failure by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val load: suspend () -> Unit = {
        try {
            // Each part shows as it arrives; the sparklines, one request a run, come last and all at once.
            coroutineScope {
                launch { agents = loadAgents(store::get, agents) }
                val ps = async { parseProjects(store.get("/api/v1/projects")) }
                val rs = parseRuns(store.get("/api/v1/runs?limit=20&metrics=1"))
                if (!loaded) runs = rs
                projects = ps.await()
                // A run that outlived twenty newer ones is not among them: ask for the running ones by status.
                runs = if (projects.sumOf { it.running } <= rs.count { it.running }) rs
                    else (parseRuns(store.get("/api/v1/runs?status=running&limit=50&metrics=1")) + rs).distinctBy { it.id }
            }
            loaded = true; error = null; okAt = System.currentTimeMillis()
            spark = coroutineScope {
                runs.filter { it.running }.map { r ->
                    async {
                        val k = r.mainMetric?.first ?: return@async null
                        runCatching { parseSeries(store.get("/api/v1/runs/${r.id}/metrics?points=40&keys=${q(k)}"))[k]!!.last.filter { it.isFinite() } }
                            .getOrNull()?.let { r.id to it } ?: spark[r.id]?.let { r.id to it }
                    }
                }.awaitAll().filterNotNull().toMap()
            }
        } catch (e: Exception) {
            error = failure(e)
        }
    }
    val running = runs.filter { it.running }
    val recent = runs.filter { !it.running }.take(8)
    Poll(Unit, { if (running.isEmpty()) 10_000 else 5_000 }, load)
    Screen("Projects") {
        Refreshable(load) {
            LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxSize()) {
                if (loaded && error != null) item { Stale(okAt, error!!) }
                if (agents.isNotEmpty()) {
                    item { SectionTitle("Agents") }
                    items(agents, key = { it.id }) { a ->
                        AgentCard(a, running.find { it.id == a.lastRun?.id }) { a.lastRun?.let { open(Route.RunR(it.id)) } }
                    }
                }
                if (running.isNotEmpty()) {
                    item { SectionTitle("Running") }
                    items(running, key = { it.id }) { r -> LiveRunCard(r, spark[r.id].orEmpty(), stale = error != null) { open(Route.RunR(r.id)) } }
                }
                if (recent.isNotEmpty()) {
                    item { SectionTitle("Recent") }
                    item {
                        Panel(padding = 0) {
                            recent.forEachIndexed { i, r ->
                                if (i > 0) HorizontalDivider(color = T.rule)
                                RunRow(r, { open(Route.RunR(r.id)) }, project = true)
                            }
                        }
                    }
                }
                item { SectionTitle("All projects") }
                if (projects.isNotEmpty()) item {
                    Panel(padding = 0) {
                        projects.forEachIndexed { i, p ->
                            if (i > 0) HorizontalDivider(color = T.rule)
                            ProjectRow(p, { open(Route.Project(p.name)) }, if (store.canDelete) ({ deleting = p }) else null)
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

/** A coding agent session, as the kiosk shows it. `run` is its last run if that is still running: only those have metrics here. */
@Composable
fun AgentCard(a: Agent, run: Run?, onClick: () -> Unit) {
    Panel(Modifier.combinedClickable(enabled = a.lastRun != null, onClick = onClick)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(a.title, fontWeight = FontWeight.SemiBold, fontSize = 16.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            Dot(T.agent(a.state))
            Text(a.word, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = T.agent(a.state), maxLines = 1, modifier = Modifier.padding(start = 6.dp))
        }
        Text(listOfNotNull(a.tool, a.project, a.host).filter { it.isNotEmpty() }.joinToString(" · "),
            fontSize = 12.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
        run?.let(a::score)?.let { (key, value) ->
            Text(fmt(value), fontSize = 24.sp, fontWeight = FontWeight.Bold, style = Num, modifier = Modifier.padding(top = 10.dp))
            Text(key, fontSize = 12.sp, color = T.muted, maxLines = 1)
        }
        a.report.ifEmpty { a.said }.takeIf { it.isNotEmpty() }?.let {
            Text(it, fontSize = 14.sp, maxLines = 3, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.dp))
        }
        Text(listOfNotNull(a.lastRun?.name, plural(a.runs, "run"), ago(a.activeAt)).joinToString(" · "),
            fontSize = 12.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.dp))
    }
}

/** A running run on the home screen: enough to see how it is going without opening it. */
@Composable
fun LiveRunCard(run: Run, spark: List<Double>, stale: Boolean = false, onClick: () -> Unit) {
    var now by remember { mutableStateOf(System.currentTimeMillis()) }
    LaunchedEffect(Unit) { while (true) { delay(1000); now = System.currentTimeMillis() } }
    val beat = remember { Animatable(1f) }
    LaunchedEffect(run.lastHeartbeatAt) { beat.animateTo(1.5f, tween(120)); beat.animateTo(1f, tween(240)) }
    val age = (now - (run.lastHeartbeatAt ?: run.startedAt)) / 1000.0
    // The library beats every 30 seconds: three missed is worth a look, long before the server's 5 minutes.
    // Not while refreshes fail: then it is the phone that hears nothing, not the run that went quiet.
    val silent = !stale && age > 90
    Panel(Modifier.combinedClickable(onClick = onClick)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("♥", fontSize = 12.sp, color = if (silent) T.status("crashed") else T.status(run.status),
                modifier = Modifier.graphicsLayer { scaleX = beat.value; scaleY = beat.value })
            Text(run.name, fontWeight = FontWeight.SemiBold, fontSize = 16.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(start = 8.dp).weight(1f))
            if (silent) Text(duration(age), fontSize = 12.sp, color = T.status("crashed"), style = Num, modifier = Modifier.padding(start = 8.dp))
            Text(duration(run.duration(now)), fontSize = 14.sp, color = T.muted, style = Num, modifier = Modifier.padding(start = 8.dp))
        }
        Text(listOfNotNull(run.project, run.host.ifEmpty { null }?.let { "on $it" }).joinToString(" · "),
            fontSize = 12.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 2.dp))
        run.mainMetric?.let { (key, value) ->
            Row(Modifier.padding(top = 10.dp), verticalAlignment = Alignment.Bottom) {
                Column(Modifier.weight(1f)) {
                    Text(fmt(value), fontSize = 24.sp, fontWeight = FontWeight.Bold, style = Num)
                    Text(if (run.progress == null) "$key · step ${run.metrics?.get(key)?.step ?: 0}" else key, fontSize = 12.sp, color = T.muted, maxLines = 1)
                }
                if (spark.size > 1) Sparkline(spark, Modifier.width(110.dp).height(34.dp))
            }
        }
        RunProgress(run)
        SysLoad.of(run.metrics)?.let { Spacer(Modifier.height(8.dp)); MachineStrip(it) }
    }
}

/** How far a run is, when its config names a total. */
@Composable
fun RunProgress(run: Run) {
    val p = run.progress ?: return
    val f = (p.done / p.total).coerceIn(0.0, 1.0)
    // Keyed on the run: the step only moves with a poll, so a ticking elapsed would push the estimate up between polls.
    // Elapsed time includes setup, so this is a rough guess.
    val left = remember(run) { if (run.running && f > 0.01 && f < 1) "~${duration(run.duration() * (1 - f) / f)} left" else null }
    Meter(f * 100, Modifier.fillMaxWidth().padding(top = 10.dp))
    Row(Modifier.padding(top = 4.dp)) {
        Text("${p.unit} ${fmt(p.done)} / ${fmt(p.total)} · ${(f * 100).roundToInt()}%", fontSize = 12.sp, color = T.muted, style = Num, maxLines = 1,
            modifier = Modifier.weight(1f))
        left?.let { Text(it, fontSize = 12.sp, color = T.muted, style = Num, maxLines = 1, modifier = Modifier.padding(start = 8.dp)) }
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
fun ProjectRow(p: Project, onClick: () -> Unit, onLongClick: (() -> Unit)?) {
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

/** One project's runs, newest first. Long-press to delete one, where the server lets the app. */
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
                            RunRow(r, { open(Route.RunR(r.id)) }, onLongClick = if (store.canDelete) ({ deleting = r }) else null)
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
fun RunRow(run: Run, onClick: () -> Unit, project: Boolean = false, onLongClick: (() -> Unit)? = null) {
    Row(Modifier.fillMaxWidth().combinedClickable(onClick = onClick, onLongClick = onLongClick).padding(horizontal = 12.dp, vertical = 11.dp),
        verticalAlignment = Alignment.CenterVertically) {
        Dot(T.status(run.status), 9)
        Column(Modifier.padding(start = 10.dp).weight(1f)) {
            Text(run.name, fontWeight = FontWeight.SemiBold, fontSize = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(listOfNotNull(if (project) run.project else null, run.status, duration(run.duration()), if (run.running) null else ago(run.startedAt)).joinToString(" · "),
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
fun AlertsScreen(store: Store, open: (Route) -> Unit, changed: suspend () -> Unit = {}) {
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
                changed() // the tab's badge
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
    var okAt by remember { mutableStateOf(0L) }
    var tab by remember { mutableStateOf("Charts") }
    val load: suspend () -> Unit = {
        try {
            run = parseRun(JSONObject(store.get("/api/v1/runs/$id")))
            series = parseSeries(store.get("/api/v1/runs/$id/metrics?points=300"))
            alerts = parseAlerts(store.get("/api/v1/alerts?run=$id&limit=10"))
            error = null; okAt = System.currentTimeMillis()
        } catch (e: Exception) { error = failure(e) }
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
                if (error != null) item { Stale(okAt, error!!) }
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
        RunProgress(run)
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
            load.ramGB?.let { Tile("RAM", "%.1f GB".format(Locale.US, it), null, Modifier.weight(1f)) }
        }
        load.gpus.forEach { g ->
            Panel {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("GPU ${g.id}", fontWeight = FontWeight.SemiBold, fontSize = 14.sp, modifier = Modifier.weight(1f))
                    g.util?.let { Text("${it.roundToInt()}%", fontWeight = FontWeight.Bold, fontSize = 18.sp, style = Num) }
                }
                g.util?.let { Meter(it, Modifier.fillMaxWidth().padding(vertical = 6.dp)) }
                Text(listOfNotNull(g.memGB?.let { "%.1f GB".format(Locale.US, it) }, g.tempC?.let { "${it.roundToInt()} °C" }, g.powerW?.let { "${it.roundToInt()} W" }).joinToString(" · "),
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

/** Images and text tables by key, with a slider through the steps; the latest step by default. Tap an image to look closely. */
@Composable
fun SamplesTab(store: Store, id: String, running: Boolean) {
    var media by remember { mutableStateOf(listOf<Media>()) }
    var loaded by remember { mutableStateOf(false) }
    val picked = remember { mutableStateMapOf<String, Int>() }
    var viewing by remember { mutableStateOf<Pair<List<Media>, Int>?>(null) }
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
                    value = index.toFloat(), valueRange = 0f..steps.lastIndex.toFloat(), steps = (steps.size - 2).coerceAtLeast(0),
                    // Left at the end it is not pinned to that step: it follows the newest one as the run logs more.
                    onValueChange = { v -> v.roundToInt().let { if (it >= steps.lastIndex) picked.remove(key) else picked[key] = it } },
                    colors = SliderDefaults.colors(thumbColor = T.ink, activeTrackColor = T.ink, inactiveTrackColor = T.rule, activeTickColor = Color.Transparent, inactiveTickColor = Color.Transparent),
                )
                val now = items.filter { it.step == steps[index] }
                val images = now.filter { it.kind == "image" || it.url.endsWith(".gif") }
                // One or two a row, so a sample is big enough to judge on a phone.
                images.chunked(2).forEach { pair ->
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        pair.forEach { m ->
                            Column(Modifier.weight(1f).combinedClickable { viewing = images to images.indexOf(m) }) {
                                RemoteImage(store, m.url)
                                m.caption?.let { Text(it, fontSize = 11.sp, color = T.muted, maxLines = 2) }
                            }
                        }
                        if (pair.size == 1 && images.size > 1) Spacer(Modifier.weight(1f))
                    }
                }
                now.filter { it.kind == "text" }.forEach { TextTable(store, it.url) }
                now.filter { it.kind == "video" && !it.url.endsWith(".gif") }.forEach {
                    Text("Video at step ${it.step}: open it on the web UI.", fontSize = 13.sp, color = T.muted)
                }
            }
        }
    }
    viewing?.let { (images, start) -> Viewer(store, images, start) { viewing = null } }
}

/** One step's images, full screen: swipe between them, pinch to zoom. */
@Composable
fun Viewer(store: Store, images: List<Media>, start: Int, onClose: () -> Unit) {
    Dialog(onClose, DialogProperties(usePlatformDefaultWidth = false)) {
        val pager = rememberPagerState(start) { images.size }
        Column(Modifier.fillMaxSize().background(T.paper)) {
            Row(Modifier.padding(start = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(listOfNotNull("${pager.currentPage + 1} / ${images.size}", images[pager.currentPage].caption).joinToString(" · "),
                    fontSize = 13.sp, color = T.muted, style = Num, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                TextButton(onClick = onClose) { Text("Close", color = T.ink) }
            }
            HorizontalPager(pager, Modifier.fillMaxSize()) { page ->
                var scale by remember { mutableStateOf(1f) }
                var offset by remember { mutableStateOf(Offset.Zero) }
                Box(Modifier.fillMaxSize().clipToBounds().pointerInput(Unit) {
                    awaitEachGesture {
                        awaitFirstDown(requireUnconsumed = false)
                        do {
                            val e = awaitPointerEvent()
                            // One finger at 1x is the pager's swipe; two fingers, or a drag once zoomed in, are the image's.
                            if (e.changes.size > 1 || scale > 1f) {
                                scale = (scale * e.calculateZoom()).coerceIn(1f, 8f)
                                val room = Offset(size.width * (scale - 1) / 2, size.height * (scale - 1) / 2)
                                offset = (offset + e.calculatePan()).let { Offset(it.x.coerceIn(-room.x, room.x), it.y.coerceIn(-room.y, room.y)) }
                                e.changes.forEach { it.consume() }
                            }
                        } while (e.changes.any { it.pressed })
                    }
                }, Alignment.Center) {
                    RemoteImage(store, images[page].url, 2048, Modifier.graphicsLayer { scaleX = scale; scaleY = scale; translationX = offset.x; translationY = offset.y })
                }
            }
        }
    }
}

/** Halves the size until the long side is under twice `max`: a camera-sized sample is too large a bitmap to draw. */
private fun decode(b: ByteArray, max: Int): Bitmap? {
    val o = BitmapFactory.Options().apply { inJustDecodeBounds = true }
    BitmapFactory.decodeByteArray(b, 0, b.size, o)
    o.inJustDecodeBounds = false
    o.inSampleSize = 1
    while (maxOf(o.outWidth, o.outHeight) / (o.inSampleSize * 2) >= max) o.inSampleSize *= 2
    return BitmapFactory.decodeByteArray(b, 0, b.size, o)
}

/** An image fetched with the session. Pixel art stays sharp. */
@Composable
fun RemoteImage(store: Store, url: String, max: Int = 1024, modifier: Modifier = Modifier.fillMaxWidth()) {
    // Not keyed on the url: when the slider moves, the old image stays until the new one has loaded.
    var image by remember { mutableStateOf<ImageBitmap?>(null) }
    LaunchedEffect(url) {
        val next = runCatching {
            val b = store.api.bytes(url)
            withContext(kotlinx.coroutines.Dispatchers.Default) { decode(b, max)?.asImageBitmap() }
        }.getOrNull()
        if (isActive) image = next // a load the slider has moved past must not blank the image
    }
    val img = image
    if (img == null) Box(modifier.aspectRatio(1f).clip(RoundedCornerShape(8.dp)).background(T.soft))
    else Image(img, null, modifier.aspectRatio(img.width.toFloat() / img.height).clip(RoundedCornerShape(8.dp)),
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
