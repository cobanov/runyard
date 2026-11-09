@file:OptIn(ExperimentalMaterial3Api::class, ExperimentalFoundationApi::class)

package dev.cobanov.runyard

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.ImageDecoder
import android.graphics.drawable.AnimatedImageDrawable
import android.graphics.drawable.Drawable
import android.net.Uri
import android.widget.ImageView
import android.widget.VideoView
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.em
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
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
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
import androidx.compose.runtime.saveable.rememberSaveable
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
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.LineHeightStyle
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
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
import kotlin.math.abs
import kotlin.math.roundToInt

// ---- small parts ----

val Mono = TextStyle(fontFamily = FontFamily.Monospace)
val Tight = (-0.02).em // DESIGN.md: negative tracking on the page title and KPI values only

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

/** The server's clock, read again every second, for what counts up on the screen. */
@Composable
fun ticking(): Long {
    var now by remember { mutableStateOf(serverNow()) }
    LaunchedEffect(Unit) { while (true) { delay(1000); now = serverNow() } }
    return now
}

/** What a failed refresh leaves in `error`; a cancelled one (the screen went away) is not a failure. */
fun failure(e: Exception): String = if (e is CancellationException) throw e else if (e is ApiError) e.message else "Can't reach the server."

/** One quiet line while refreshes fail: what is on the screen is as old as it says. */
@Composable
fun Stale(since: Long, error: String) {
    var now by remember { mutableStateOf(System.currentTimeMillis()) }
    LaunchedEffect(Unit) { while (true) { delay(1000); now = System.currentTimeMillis() } }
    Text("Not updated for ${duration((now - since) / 1000.0)}. $error", fontSize = 13.sp, color = T.crashed)
}

/**
 * A tab's screen has the page title with one quiet line under it. A pushed screen has a back arrow and a small
 * title; `quiet` when the content carries the headline itself.
 */
@Composable
fun Screen(
    title: String, back: (() -> Unit)? = null, quiet: Boolean = false, sub: @Composable () -> Unit = {},
    actions: @Composable () -> Unit = {}, content: @Composable () -> Unit,
) {
    Column(Modifier.fillMaxSize()) {
        if (back != null) TopAppBar(
            title = {
                Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis, fontSize = if (quiet) 15.sp else 18.sp,
                    color = if (quiet) T.muted else T.ink, fontWeight = if (quiet) FontWeight.Normal else FontWeight.SemiBold)
            },
            navigationIcon = { IconButton(onClick = back) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") } },
            actions = { actions() }, windowInsets = WindowInsets(0),
            colors = TopAppBarDefaults.topAppBarColors(containerColor = T.paper, scrolledContainerColor = T.paper),
        ) else Box(Modifier.fillMaxWidth().padding(start = 16.dp, end = 4.dp, top = 16.dp, bottom = 6.dp)) {
            Column {
                Text(title, fontSize = 24.sp, fontWeight = FontWeight.Bold, letterSpacing = Tight, modifier = Modifier.semantics { heading() })
                sub()
            }
            // On the title's line, without moving it: a 48dp button is taller than the title it sits beside.
            Box(Modifier.align(Alignment.TopEnd).offset(y = (-7).dp)) { actions() }
        }
        content()
    }
}

/** What every list on a screen shares: the 16dp gutter, 14 between cards. */
val ListPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 6.dp, bottom = 24.dp)
val ListGap = Arrangement.spacedBy(14.dp)

@Composable
fun Refreshable(load: suspend () -> Unit, content: @Composable () -> Unit) {
    var refreshing by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    PullToRefreshBox(refreshing, onRefresh = { scope.launch { refreshing = true; load(); refreshing = false } }, Modifier.fillMaxSize()) { content() }
}

/** DESIGN.md's one surface: a tone up from the ground, a hairline edge, no shadow. A click given in `modifier` stays inside the corners. */
@Composable
fun Panel(modifier: Modifier = Modifier, padding: Int = 14, content: @Composable ColumnScope.() -> Unit) {
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).background(T.panel).border(1.dp, T.rule, RoundedCornerShape(8.dp)).then(modifier).padding(padding.dp),
        content = content,
    )
}

/** A heading belongs to what follows it: in a list spaced by 14 it sits 24 under the last card and 8 over the next. */
@Composable
fun SectionTitle(text: String) =
    Text(text, fontSize = 13.sp, color = T.muted, modifier = Modifier.padding(top = 4.dp).offset(y = 6.dp).semantics { heading() })

/** A state's mark. Only a running one breathes; nothing else on a screen moves. */
@Composable
fun Dot(color: Color, size: Int = 8, breathe: Boolean = false) {
    val a = if (breathe) rememberInfiniteTransition(label = "breathe").animateFloat(1f, 0.35f, infiniteRepeatable(tween(1100), RepeatMode.Reverse), label = "alpha") else null
    Box(Modifier.size(size.dp).graphicsLayer { alpha = a?.value ?: 1f }.clip(CircleShape).background(color))
}

/** A row of a key and what was recorded for it, on one baseline: the two are different typefaces. */
@Composable
fun KeyValue(key: String, value: String) {
    SelectionContainer {
        Row(Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = 10.dp)) {
            Text(key, fontSize = 13.sp, color = T.muted, style = Mono, modifier = Modifier.widthIn(max = 170.dp).padding(end = 12.dp).alignByBaseline())
            Text(value, fontSize = 14.sp, textAlign = TextAlign.End, modifier = Modifier.weight(1f).alignByBaseline())
        }
    }
}

/** An empty list, or one that could not load: one sentence in DESIGN.md's dashed box, saying what would put something there. */
@Composable
fun Empty(text: String) {
    val rule = T.rule
    Text(text, color = T.muted, fontSize = 14.sp, modifier = Modifier.fillMaxWidth().drawBehind {
        drawRoundRect(rule, cornerRadius = CornerRadius(8.dp.toPx()), style = Stroke(1.dp.toPx(), pathEffect = PathEffect.dashPathEffect(floatArrayOf(4.dp.toPx(), 4.dp.toPx()))))
    }.padding(horizontal = 14.dp, vertical = 10.dp))
}

/** While the first answer is on its way: nothing for 300 ms, so a quick server flashes no placeholder, then one quiet word. */
@Composable
fun Loading() {
    var late by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { delay(300); late = true }
    Text(if (late) "Loading…" else "", color = T.muted, fontSize = 14.sp)
}

fun ago(t: Long): String = ((serverNow() - t) / 1000.0).let { if (it < 60) "just now" else "${brief(it)} ago" }
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

/**
 * The servers the app has connected to: tap one to go back to it as it was left, long-press to forget it. With
 * `add` (the Server tab) the current one carries a check mark and a last row adds another; without, rows lead on.
 */
@Composable
fun ServerList(store: Store, add: (() -> Unit)? = null) {
    var forgetting by remember { mutableStateOf<String?>(null) }
    val row = Modifier.fillMaxWidth().heightIn(min = 48.dp).padding(horizontal = 14.dp, vertical = 10.dp)
    Panel(padding = 0) {
        store.servers.forEachIndexed { i, s ->
            if (i > 0) HorizontalDivider(color = T.rule)
            Row(Modifier.combinedClickable(onLongClickLabel = "Forget", onLongClick = { forgetting = s }) { store.switch(s) }.then(row), verticalAlignment = Alignment.CenterVertically) {
                Text(s, fontSize = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                if (add == null) Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, Modifier.size(20.dp), tint = T.muted)
                else if (s == store.server) Icon(Icons.Filled.Check, "Connected", Modifier.size(20.dp), tint = T.ink)
            }
        }
        if (add != null) {
            HorizontalDivider(color = T.rule)
            Row(Modifier.clickable(onClick = add).then(row), verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Filled.Add, null, Modifier.size(18.dp), tint = T.muted)
                Text("Add a server", fontSize = 15.sp, modifier = Modifier.padding(start = 8.dp))
            }
        }
    }
    forgetting?.let { s ->
        DeleteDialog("Forget $s?", "Its address and your session there leave this phone. Nothing changes on the server.", "Forget", { forgetting = null }) { store.remove(s) }
    }
}

// ---- home ----

/** Home: what needs a look, what is training now, each at a glance, the coding agents, the runs that just ended, then every project. */
@Composable
fun HomeScreen(store: Store, alerts: List<Alert>, toAlerts: () -> Unit, open: (Route) -> Unit) {
    var runs by remember { mutableStateOf(listOf<Run>()) }
    var okAt by remember { mutableStateOf(0L) }
    var spark by remember { mutableStateOf(mapOf<String, List<Double>>()) }
    var thumbs by remember { mutableStateOf(mapOf<String, String>()) }
    var projects by remember { mutableStateOf(listOf<Project>()) }
    var agents by remember { mutableStateOf(listOf<Agent>()) }
    var loaded by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var deleting by remember { mutableStateOf<Project?>(null) }
    var failure by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    // What the last look left on the device, for a launch without the server; the line on top says how old it is.
    val kept = {
        runCatching {
            val (rs, at) = store.kept("/api/v1/runs?limit=20&metrics=1")!!
            projects = parseProjects(store.kept("/api/v1/projects")!!.first)
            agents = store.kept("/api/v1/agents?hours=24")?.let { parseAgents(it.first) }.orEmpty()
            runs = parseRuns(rs); okAt = at; loaded = true
        }
    }
    val load: suspend () -> Unit = {
        // The launch already found the server gone: show the kept screen now, not after this try times out too.
        if (!loaded && store.loadError != null) { error = "Can't reach the server."; kept() }
        try {
            // Each part shows as it arrives; the sparklines and latest samples, a request a run each, come last and all at once.
            coroutineScope {
                launch { agents = loadAgents({ store.get(it, keep = true) }, agents) }
                val ps = async { parseProjects(store.get("/api/v1/projects", keep = true)) }
                val rs = parseRuns(store.get("/api/v1/runs?limit=20&metrics=1", keep = true))
                if (!loaded) runs = rs
                projects = ps.await()
                // A run that outlived twenty newer ones is not among them: ask for the running ones by status.
                runs = if (projects.sumOf { it.running } <= rs.count { it.running }) rs
                    else (parseRuns(store.get("/api/v1/runs?status=running&limit=50&metrics=1")) + rs).distinctBy { it.id }
            }
            loaded = true; error = null; okAt = System.currentTimeMillis()
            // A request that fails keeps what the card showed before.
            suspend fun <V> each(old: Map<String, V>, one: suspend (Run) -> V?) = coroutineScope {
                runs.filter { it.running }.map { r -> async { (runCatching { one(r) }.getOrNull() ?: old[r.id])?.let { r.id to it } } }.awaitAll().filterNotNull().toMap()
            }
            coroutineScope {
                launch {
                    spark = each(spark) { r ->
                        r.mainMetric?.first?.let { k -> parseSeries(store.get("/api/v1/runs/${r.id}/metrics?points=40&keys=${q(k)}"))[k]!!.last.filter { it.isFinite() } }
                    }
                }
                launch { thumbs = each(thumbs) { r -> latestImage(parseMedia(store.get("/api/v1/runs/${r.id}/media?kind=image&latest")))?.url } }
            }
        } catch (e: Exception) {
            error = failure(e)
            if (!loaded) kept()
        }
    }
    val running = runs.filter { it.running }
    val recent = runs.filter { !it.running }.take(8)
    Poll(Unit, { if (running.isEmpty()) 10_000 else 5_000 }, load)
    Screen("Runs", sub = {
        // The line is there before the answer, so nothing moves down when it arrives.
        if (loaded && error != null) Stale(okAt, error!!) else Text(if (loaded) summary(runs) else "", fontSize = 13.sp, color = T.muted)
    }) {
        Refreshable(load) {
            LazyColumn(contentPadding = ListPadding, verticalArrangement = ListGap, modifier = Modifier.fillMaxSize()) {
                if (alerts.isNotEmpty()) item(key = "alerts") { AlertStrip(alerts, toAlerts) }
                if (running.isNotEmpty()) {
                    item { SectionTitle("Running") }
                    items(running, key = { it.id }) { r ->
                        LiveRunCard(store, r, spark[r.id].orEmpty(), thumbs[r.id], stale = error != null, onSamples = { open(Route.RunR(r.id, samples = true)) }) { open(Route.RunR(r.id)) }
                    }
                }
                if (agents.isNotEmpty()) {
                    item { SectionTitle("Agents") }
                    items(agents, key = { it.id }) { a ->
                        AgentCard(a, running.find { it.id == a.lastRun?.id }) { a.lastRun?.let { open(Route.RunR(it.id)) } }
                    }
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
                if (projects.isNotEmpty()) {
                    item { SectionTitle("Projects") }
                    item {
                        Panel(padding = 0) {
                            projects.forEachIndexed { i, p ->
                                if (i > 0) HorizontalDivider(color = T.rule)
                                ProjectRow(p, { open(Route.Project(p.name)) }, if (store.canDelete) ({ deleting = p }) else null)
                            }
                        }
                    }
                }
                if (loaded && projects.isEmpty()) item { Empty("No runs yet. They show up here once a training script calls runyard.init().") }
                else if (!loaded && error != null) item { Empty(error!!) }
                else if (!loaded) item { Loading() }
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

/** The alerts nobody dismissed yet, in one row: the newest, and how many more there are. */
@Composable
fun AlertStrip(alerts: List<Alert>, onClick: () -> Unit) {
    val a = alerts.maxBy { it.ts }
    Panel(Modifier.clickable(onClickLabel = "Alerts", onClick = onClick), padding = 0) {
        Row(Modifier.heightIn(min = 48.dp).padding(horizontal = 14.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            Dot(T.level(a.level))
            Text(buildAnnotatedString {
                withStyle(SpanStyle(color = T.ink, fontWeight = FontWeight.SemiBold)) { append(a.title) }
                append(" · ${a.runName} · ${ago(a.ts)}")
            }, fontSize = 14.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 10.dp).weight(1f))
            if (alerts.size > 1) Text("+${alerts.size - 1} more", fontSize = 13.sp, color = T.muted, maxLines = 1, modifier = Modifier.padding(start = 10.dp))
        }
    }
}

/** A coding agent session, as the kiosk shows it. `run` is its last run if that is still running: only those have metrics here. */
@Composable
fun AgentCard(a: Agent, run: Run?, onClick: () -> Unit) {
    Panel(Modifier.clickable(enabled = a.lastRun != null, onClick = onClick)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(a.title, fontWeight = FontWeight.SemiBold, fontSize = 16.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            Dot(T.agent(a.state))
            Text(a.word, fontSize = 13.sp, color = T.agent(a.state), maxLines = 1, modifier = Modifier.padding(start = 6.dp))
        }
        Text(listOfNotNull(a.tool, a.project, a.host).filter { it.isNotEmpty() }.joinToString(" · "),
            fontSize = 13.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 2.dp))
        run?.let(a::score)?.let { (key, value) -> Kpi(fmt(value), key, value.isFinite(), Modifier.padding(top = 12.dp)) }
        a.report.ifEmpty { a.said }.takeIf { it.isNotEmpty() }?.let {
            Text(it, fontSize = 14.sp, maxLines = 3, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 10.dp))
        }
        Text(listOfNotNull(a.lastRun?.name, plural(a.runs, "run"), ago(a.activeAt)).joinToString(" · "),
            fontSize = 13.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 10.dp))
    }
}

/** DESIGN.md's KPI: the value at 28/700 with its name under it. A NaN or an infinity is the one number that is red. */
@Composable
fun Kpi(value: String, name: String, ok: Boolean = true, modifier: Modifier = Modifier) {
    Column(modifier) {
        Text(value, fontSize = 28.sp, fontWeight = FontWeight.Bold, letterSpacing = Tight, lineHeight = 1.2.em, maxLines = 1, color = if (ok) T.ink else T.failed)
        Text(name, fontSize = 13.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** A running run on the home screen: enough to see how it is going without opening it. */
@Composable
fun LiveRunCard(store: Store, run: Run, spark: List<Double>, sample: String?, stale: Boolean = false, onSamples: () -> Unit, onClick: () -> Unit) {
    val now = ticking()
    // The library beats every 30 seconds: three missed is worth a look, long before the server's 5 minutes. A run
    // that beats but has logged nothing for ten minutes is stuck. Not while refreshes fail: then it is the phone
    // that hears nothing, not the run that went quiet.
    val beat = (now - (run.lastHeartbeatAt ?: run.startedAt)) / 1000.0
    val idle = (now - (run.progressAt ?: now)) / 1000.0
    val silent = if (stale) null else if (beat > 90) "no heartbeat for ${brief(beat)}" else if (idle > 600) "no output for ${brief(idle)}" else null
    Panel(Modifier.clickable(onClick = onClick)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Dot(if (silent == null) T.running else T.crashed, breathe = silent == null)
            Text(run.name, fontWeight = FontWeight.SemiBold, fontSize = 16.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(start = 8.dp).weight(1f))
            Text(duration(run.duration(now)), fontSize = 13.sp, color = T.muted, maxLines = 1, modifier = Modifier.padding(start = 8.dp))
        }
        Text(buildAnnotatedString {
            silent?.let { withStyle(SpanStyle(color = T.crashed)) { append(it) }; append(" · ") }
            append(listOf(run.project, run.host).filter { it.isNotEmpty() }.joinToString(" · "))
        }, fontSize = 13.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 2.dp))
        val main = run.mainMetric
        if (main != null || sample != null) Row(Modifier.padding(top = 12.dp).height(IntrinsicSize.Min), verticalAlignment = Alignment.CenterVertically) {
            main?.let { (key, value) -> Kpi(fmt(value), if (run.progress == null) "$key · step ${count((run.metrics?.get(key)?.step ?: 0).toDouble())}" else key, value.isFinite()) }
            // The sparkline is as tall as the number's block and takes what is left between it and the sample.
            if (spark.size > 1) Sparkline(spark, Modifier.weight(1f).fillMaxHeight().padding(horizontal = 14.dp, vertical = 8.dp)) else Spacer(Modifier.weight(1f))
            sample?.let {
                RemoteImage(store, thumb(it, 320), 320, Modifier.size(64.dp).clickable(onClickLabel = "Open the samples", onClick = onSamples), square = true, description = "Latest sample")
            }
        }
        RunProgress(run)
        SysLoad.of(run.metrics)?.line?.takeIf { it.isNotEmpty() }?.let {
            Text(it, fontSize = 13.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 2.dp))
        }
    }
}

/** How far a run is, when its config names a total. */
@Composable
fun RunProgress(run: Run) {
    val p = run.progress ?: return
    val f = (p.done / p.total).coerceIn(0.0, 1.0)
    // Keyed on the run: the step only moves with a poll, so a ticking elapsed would push the estimate up between polls.
    // Elapsed time includes setup, so this is a rough guess.
    val left = remember(run) { if (run.running && f > 0.01 && f < 1) "~${brief(run.duration() * (1 - f) / f)} left" else null }
    Meter(f * 100, Modifier.fillMaxWidth().padding(top = 12.dp), T.status(run.status))
    Row(Modifier.padding(top = 6.dp)) {
        Text("${p.unit} ${count(p.done)} / ${count(p.total)} · ${pct(f * 100)}", fontSize = 13.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f))
        left?.let { Text(it, fontSize = 13.sp, color = T.muted, maxLines = 1, modifier = Modifier.padding(start = 8.dp)) }
    }
}

/** A 4dp bar. A load is one neutral colour whatever its value; a run's progress wears the run's state. */
@Composable
fun Meter(percent: Double, modifier: Modifier = Modifier, color: Color = T.muted) {
    LinearProgressIndicator(
        progress = { (percent / 100).toFloat().coerceIn(0f, 1f) }, modifier = modifier.height(4.dp).clip(RoundedCornerShape(2.dp)),
        color = color, trackColor = T.soft, gapSize = 0.dp, drawStopIndicator = {},
    )
}

@Composable
fun Sparkline(values: List<Double>, modifier: Modifier) {
    val color = T.c1
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

/** What a list row shares: the gutter, and a height a thumb can hit. */
private val RowPad = Modifier.fillMaxWidth().heightIn(min = 56.dp).padding(horizontal = 14.dp, vertical = 10.dp)

@Composable
fun ProjectRow(p: Project, onClick: () -> Unit, onLongClick: (() -> Unit)?) {
    Row(Modifier.combinedClickable(onClick = onClick, onLongClick = onLongClick, onLongClickLabel = "Delete").then(RowPad), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(p.name, fontWeight = FontWeight.SemiBold, fontSize = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(buildAnnotatedString {
                if (p.running > 0) { withStyle(SpanStyle(color = T.running)) { append("${p.running} running") }; append(" · ") }
                append(listOfNotNull(plural(p.runs, "run"), p.lastRunAt?.let { "last " + ago(it) }).joinToString(" · "))
            }, fontSize = 13.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, Modifier.size(20.dp), tint = T.muted)
    }
}

// ---- a project's runs ----

/** One project's runs, newest first. Long-press to delete one, where the server lets the app. */
@Composable
fun RunsScreen(store: Store, project: String, open: (Route) -> Unit, back: () -> Unit) {
    var runs by remember { mutableStateOf(listOf<Run>()) }
    var loaded by remember { mutableStateOf(false) }
    var okAt by remember { mutableStateOf(0L) }
    var error by remember { mutableStateOf<String?>(null) }
    var deleting by remember { mutableStateOf<Run?>(null) }
    var failure by remember { mutableStateOf<String?>(null) }
    var query by rememberSaveable { mutableStateOf("") }
    val scope = rememberCoroutineScope()
    val load: suspend () -> Unit = {
        try {
            runs = parseRuns(store.get("/api/v1/runs?project=${q(project)}&limit=200&metrics=1"))
            loaded = true; error = null; okAt = System.currentTimeMillis()
        } catch (e: Exception) { error = failure(e) }
    }
    Poll(project, { if (runs.any { it.running }) 10_000 else 30_000 }, load)
    val found = query.trim().let { w -> runs.filter { r -> r.name.contains(w, true) || r.tags.any { it.contains(w, true) } } }
    Screen(project, back) {
        Refreshable(load) {
            LazyColumn(contentPadding = ListPadding, verticalArrangement = ListGap, modifier = Modifier.fillMaxSize()) {
                if (loaded && error != null) item { Stale(okAt, error!!) }
                if (runs.size > 1) item { Field(query, { query = it }, "Search runs by name or tag") }
                if (runs.isNotEmpty() && found.isEmpty()) item { Empty("No run has that in its name or tags.") }
                if (found.isNotEmpty()) item {
                    Panel(padding = 0) {
                        found.forEachIndexed { i, r ->
                            if (i > 0) HorizontalDivider(color = T.rule)
                            RunRow(r, { open(Route.RunR(r.id)) }, onLongClick = if (store.canDelete) ({ deleting = r }) else null)
                        }
                    }
                }
                if (loaded && runs.isEmpty()) item { Empty("No runs in this project.") }
                else if (!loaded && error != null) item { Empty(error!!) }
                else if (!loaded) item { Loading() }
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
    Row(Modifier.combinedClickable(onClick = onClick, onLongClick = onLongClick, onLongClickLabel = "Delete").then(RowPad), verticalAlignment = Alignment.Top) {
        Box(Modifier.padding(top = 7.dp)) { Dot(T.status(run.status), breathe = run.running) } // on the name's line
        Column(Modifier.padding(start = 10.dp).weight(1f)) {
            Text(run.name, fontWeight = FontWeight.SemiBold, fontSize = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(buildAnnotatedString {
                // Finished is the quiet default and goes unsaid; any other state is named, in its colour.
                if (run.status != "finished") { withStyle(SpanStyle(color = T.status(run.status))) { append(run.status) }; append(" · ") }
                append(listOfNotNull(if (project) run.project else duration(run.duration()),
                    if (run.running) null else ago(run.endedAt ?: run.lastHeartbeatAt ?: run.startedAt)).joinToString(" · "))
            }, fontSize = 13.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (!project && run.tags.isNotEmpty()) Text(run.tags.joinToString("  "), fontSize = 13.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        run.mainMetric?.let { (k, v) ->
            Column(horizontalAlignment = Alignment.End, modifier = Modifier.padding(start = 12.dp)) {
                Text(fmt(v), fontWeight = FontWeight.SemiBold, fontSize = 15.sp, color = if (v.isFinite()) T.ink else T.failed)
                Text(k, fontSize = 12.sp, color = T.muted, maxLines = 1)
            }
        }
    }
}

// ---- alerts ----

@Composable
fun AlertsScreen(store: Store, open: (Route) -> Unit, changed: suspend () -> Unit = {}) {
    var alerts by remember { mutableStateOf(listOf<Alert>()) }
    var loaded by remember { mutableStateOf(false) }
    var okAt by remember { mutableStateOf(0L) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val load: suspend () -> Unit = {
        try {
            alerts = parseAlerts(store.get("/api/v1/alerts?limit=100")).sortedByDescending { it.ts }
            loaded = true; error = null; okAt = System.currentTimeMillis()
        } catch (e: Exception) { error = failure(e) }
    }
    Poll(Unit, { 15_000 }, load)
    Screen("Alerts", sub = { if (loaded && error != null) Stale(okAt, error!!) }, actions = {
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
            LazyColumn(contentPadding = ListPadding, verticalArrangement = ListGap, modifier = Modifier.fillMaxSize()) {
                if (alerts.isNotEmpty()) item {
                    Panel(padding = 0) {
                        alerts.forEachIndexed { i, a ->
                            if (i > 0) HorizontalDivider(color = T.rule)
                            Box(Modifier.fillMaxWidth().clickable { open(Route.RunR(a.run)) }.alpha(if (a.dismissedAt == null) 1f else 0.5f).padding(horizontal = 14.dp, vertical = 12.dp)) {
                                AlertRow(a, showRun = true)
                            }
                        }
                    }
                }
                if (loaded && alerts.isEmpty()) item { Empty("No alerts. run.alert() in a script and the server's own checks, a NaN loss or a stalled run, raise them.") }
                else if (!loaded && error != null) item { Empty(error!!) }
                else if (!loaded) item { Loading() }
            }
        }
    }
}

/** The level as a dot, the title, where and when, then the first line of what it said. */
@Composable
fun AlertRow(alert: Alert, showRun: Boolean = false) {
    Row(verticalAlignment = Alignment.Top) {
        Box(Modifier.padding(top = 7.dp)) { Dot(T.level(alert.level)) }
        Column(Modifier.padding(start = 10.dp)) {
            Text(alert.title, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
            Text((if (showRun) listOf(alert.runName, alert.project) else emptyList<String>()).plus(ago(alert.ts)).filter { it.isNotEmpty() }.joinToString(" · "),
                fontSize = 13.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            alert.body.lineSequence().firstOrNull()?.takeIf { it.isNotEmpty() }?.let {
                Text(it, fontSize = 14.sp, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 4.dp))
            }
        }
    }
}

// ---- a run ----

// Samples second: for an image model they matter more than anything after the loss.
private val tabs = listOf("Charts", "Samples", "Logs", "System", "Config")

/** One run, with the web UI's tabs. `samples` opens it on them: the tap on a running card's thumbnail. */
@Composable
fun RunScreen(store: Store, id: String, back: () -> Unit, samples: Boolean = false) {
    var run by remember { mutableStateOf<Run?>(null) }
    var series by remember { mutableStateOf(mapOf<String, Series>()) }
    var alerts by remember { mutableStateOf(listOf<Alert>()) }
    var error by remember { mutableStateOf<String?>(null) }
    var okAt by remember { mutableStateOf(0L) }
    var tab by rememberSaveable { mutableStateOf(if (samples) "Samples" else "Charts") }
    val load: suspend () -> Unit = {
        try {
            run = parseRun(JSONObject(store.get("/api/v1/runs/$id")))
            series = parseSeries(store.get("/api/v1/runs/$id/metrics?points=300"))
            alerts = parseAlerts(store.get("/api/v1/alerts?run=$id&limit=10"))
            error = null; okAt = System.currentTimeMillis()
        } catch (e: Exception) { error = failure(e) }
    }
    Poll(id, { if (run?.running == false) 60_000 else 5_000 }, load)
    // The main metric, a loss, is the first chart; the rest follow by name.
    val main = run?.mainMetric?.first
    val trainKeys = series.keys.filter { !it.startsWith("sys/") }.sortedWith(compareBy({ it != main }, { it }))
    val sysKeys = series.keys.filter { it.startsWith("sys/") }.sorted()
    val shown = tabs.filter { it != "System" || sysKeys.isNotEmpty() }
    // The bar names the project, quietly; the run's name is the headline below, once.
    Screen(run?.project.orEmpty(), back, quiet = true) {
        val r = run
        if (r == null) {
            Box(Modifier.padding(16.dp)) { if (error != null) Empty(error!!) else Loading() }
            return@Screen
        }
        val head: @Composable () -> Unit = {
            Column(verticalArrangement = ListGap) {
                if (error != null) Stale(okAt, error!!)
                RunHeader(r)
                if (alerts.isNotEmpty()) Panel { alerts.forEachIndexed { i, a -> if (i > 0) Spacer(Modifier.height(12.dp)); AlertRow(a) } }
                Tabs(shown, tab) { tab = it }
            }
        }
        // Logs take what is left of the screen and scroll on their own; every other tab scrolls with the header.
        if (tab == "Logs") Column(Modifier.fillMaxSize().padding(start = 16.dp, end = 16.dp, bottom = 16.dp), verticalArrangement = ListGap) {
            head()
            LogsTab(store, id, r.running, Modifier.weight(1f))
        } else Refreshable(load) {
            LazyColumn(contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp), verticalArrangement = ListGap, modifier = Modifier.fillMaxSize()) {
                item { head() }
                when (tab) {
                    "Charts" -> if (trainKeys.isEmpty()) item { Empty("No metrics yet. They appear as soon as the script calls run.log().") }
                        else items(trainKeys, key = { it }) { k -> MetricChart(k, series.getValue(k)) }
                    "System" -> {
                        SysLoad.of(r.metrics)?.let { item { SystemNow(it) } }
                        items(sysKeys, key = { it }) { k -> MetricChart(k.removePrefix("sys/"), series.getValue(k), seconds = true) }
                    }
                    "Samples" -> item { SamplesTab(store, id, r.running) }
                    else -> item { ConfigTab(r) }
                }
            }
        }
    }
}

@Composable
fun RunHeader(run: Run) {
    val now = ticking()
    Column {
        Text(run.name, fontSize = 24.sp, fontWeight = FontWeight.Bold, letterSpacing = Tight, lineHeight = 1.2.em, modifier = Modifier.semantics { heading() })
        Row(Modifier.padding(top = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            Dot(T.status(run.status), breathe = run.running)
            Text(run.status, color = T.status(run.status), fontWeight = FontWeight.SemiBold, fontSize = 15.sp, modifier = Modifier.padding(start = 6.dp))
            Text(" · ${duration(run.duration(now))}", fontSize = 15.sp, color = T.muted)
        }
        listOfNotNull(run.host.ifEmpty { null }, run.lastHeartbeatAt?.takeIf { run.running }?.let { "heartbeat ${duration((now - it) / 1000.0)} ago" })
            .takeIf { it.isNotEmpty() }?.let { Text(it.joinToString(" · "), fontSize = 13.sp, color = T.muted, modifier = Modifier.padding(top = 2.dp)) }
        RunProgress(run)
        if (run.notes.isNotEmpty()) Text(run.notes, fontSize = 14.sp, modifier = Modifier.padding(top = 10.dp))
    }
}

/** DESIGN.md's tabs: text, a 2dp ink line under the active one, a hairline under them all. */
@Composable
private fun Tabs(all: List<String>, tab: String, pick: (String) -> Unit) {
    val ink = T.ink
    val pad = 10.dp
    Column {
        // Pulled left by the padding, so the first word starts on the gutter; scrolls only when a large font needs it.
        Row(Modifier.offset(x = -pad).horizontalScroll(rememberScrollState()).selectableGroup()) {
            all.forEach { t ->
                val on = t == tab
                Text(t, fontSize = 15.sp, color = if (on) T.ink else T.muted, maxLines = 1, modifier = Modifier
                    .selectable(on, role = Role.Tab) { pick(t) }
                    .drawBehind { if (on) drawRect(ink, Offset(pad.toPx(), size.height - 2.dp.toPx()), Size(size.width - 2 * pad.toPx(), 2.dp.toPx())) }
                    .padding(horizontal = pad, vertical = 12.dp))
            }
        }
        HorizontalDivider(color = T.rule)
    }
}

/**
 * A metric's line, with the min–max band of each bucket when the server thinned it out. The name and the latest
 * value share a line; a finger on the plot turns that into the value under it, and says which step.
 */
@Composable
fun MetricChart(title: String, s: Series, seconds: Boolean = false) {
    data class P(val step: Int, val last: Double, val min: Double, val max: Double)
    val pts = remember(s) { s.step.indices.map { P(s.step[it], s.last[it], s.min.getOrElse(it) { s.last[it] }, s.max.getOrElse(it) { s.last[it] }) }.filter { it.last.isFinite() } }
    val band = pts.any { it.min != it.max && it.min.isFinite() && it.max.isFinite() }
    var touch by remember { mutableStateOf<Float?>(null) }
    var plot by remember { mutableStateOf(1f) } // the plot's width, without the labels beside it
    val lo0 = pts.minOfOrNull { if (band && it.min.isFinite()) minOf(it.min, it.last) else it.last } ?: 0.0
    val hi0 = pts.maxOfOrNull { if (band && it.max.isFinite()) maxOf(it.max, it.last) else it.last } ?: 1.0
    val pad = if (hi0 > lo0) (hi0 - lo0) * 0.06 else 0.5 // the line never touches the edges
    val lo = lo0 - pad
    val hi = hi0 + pad
    val x0 = pts.firstOrNull()?.step ?: 0
    val x1 = pts.lastOrNull()?.step ?: 1
    val pick = touch?.let { tx -> val step = x0 + (x1 - x0) * (tx / plot).coerceIn(0f, 1f); pts.minByOrNull { abs(it.step - step) } }
    val t = T
    val measurer = rememberTextMeasurer()
    fun at(step: Int) = if (seconds) duration(step.toDouble()) else count(step.toDouble())
    Panel {
        Row(verticalAlignment = Alignment.Top) {
            Column(Modifier.weight(1f)) {
                Text(title, fontWeight = FontWeight.SemiBold, fontSize = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                // The line is always there, empty: the card must not grow under the finger.
                Text(pick?.let { if (seconds) "after ${at(it.step)}" else "step ${at(it.step)}" }.orEmpty(), fontSize = 12.sp, color = t.muted, maxLines = 1)
            }
            (pick ?: pts.lastOrNull())?.let { Text(fmt(it.last), fontWeight = FontWeight.SemiBold, fontSize = 17.sp, maxLines = 1, modifier = Modifier.padding(start = 12.dp)) }
        }
        Canvas(Modifier.fillMaxWidth().padding(top = 2.dp).height(170.dp).pointerInput(pts) {
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
            val label = TextStyle(fontSize = 12.sp, color = t.muted, fontFeatureSettings = "tnum")
            val marks = ticks(lo, hi).map { it to measurer.measure(fmt(it), label) }
            val foot = measurer.measure("0", label).size.height
            // Values at the right, steps under the plot: no axis lines, only the hairlines across.
            val w = size.width - marks.maxOf { it.second.size.width } - 8.dp.toPx()
            val h = size.height - foot - 6.dp.toPx()
            plot = w
            fun x(step: Int) = w * (if (x1 > x0) (step - x0).toFloat() / (x1 - x0) else 0.5f)
            fun y(v: Double) = (h * (1 - (v - lo) / (hi - lo))).toFloat()
            marks.forEach { (v, m) ->
                drawLine(t.rule, Offset(0f, y(v)), Offset(w, y(v)), 1.dp.toPx())
                drawText(m, topLeft = Offset(size.width - m.size.width, (y(v) - m.size.height / 2f).coerceIn(0f, h - m.size.height / 2f)))
            }
            // The first, the middle and the last step, and no more.
            if (x1 > x0) listOf(x0 to 0f, (x0 + x1) / 2 to 0.5f, x1 to 1f).forEach { (step, f) ->
                val m = measurer.measure(at(step), label)
                drawText(m, topLeft = Offset((w - m.size.width) * f, size.height - m.size.height))
            }
            if (pts.isEmpty()) return@Canvas
            if (band) {
                val area = Path()
                pts.forEachIndexed { i, p -> if (i == 0) area.moveTo(x(p.step), y(p.max)) else area.lineTo(x(p.step), y(p.max)) }
                pts.asReversed().forEach { p -> area.lineTo(x(p.step), y(p.min)) }
                area.close()
                drawPath(area, t.c1.copy(alpha = 0.14f))
            }
            val line = Path()
            pts.forEachIndexed { i, p -> if (i == 0) line.moveTo(x(p.step), y(p.last)) else line.lineTo(x(p.step), y(p.last)) }
            drawPath(line, t.c1, style = Stroke(1.5.dp.toPx(), join = StrokeJoin.Round))
            pick?.let {
                drawLine(t.muted, Offset(x(it.step), 0f), Offset(x(it.step), h), 1.dp.toPx())
                drawCircle(t.panel, 5.dp.toPx(), Offset(x(it.step), y(it.last)))
                drawCircle(t.c1, 3.5.dp.toPx(), Offset(x(it.step), y(it.last)))
            }
        }
        if (pts.size < s.step.size) Text("${s.step.size - pts.size} NaN or infinite values not drawn", fontSize = 12.sp, color = t.crashed, modifier = Modifier.padding(top = 6.dp))
    }
}

/** The latest readings of the SDK's system monitor: the machine's load at a glance, one row per GPU. */
@Composable
fun SystemNow(load: SysLoad) {
    Column(verticalArrangement = ListGap) {
        if (load.cpu != null || load.ramGB != null) Row(horizontalArrangement = ListGap) {
            load.cpu?.let { Tile("CPU", pct(it), it, Modifier.weight(1f)) }
            load.ramGB?.let { Tile("RAM", gb(it), null, Modifier.weight(1f)) }
        }
        load.gpus.forEach { g ->
            Panel {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("GPU ${g.id}", fontWeight = FontWeight.SemiBold, fontSize = 15.sp, modifier = Modifier.weight(1f))
                    g.util?.let { Text(pct(it), fontWeight = FontWeight.SemiBold, fontSize = 17.sp) }
                }
                g.util?.let { Meter(it, Modifier.fillMaxWidth().padding(top = 10.dp, bottom = 8.dp)) }
                Text(listOfNotNull(g.memGB?.let(::gb), g.tempC?.let { "${it.roundToInt()} °C" }, g.powerW?.let { "${it.roundToInt()} W" }).joinToString(" · "),
                    fontSize = 13.sp, color = T.muted)
            }
        }
    }
}

@Composable
private fun Tile(title: String, value: String, load: Double?, modifier: Modifier) {
    Panel(modifier) {
        Text(title, fontSize = 13.sp, color = T.muted)
        Text(value, fontWeight = FontWeight.SemiBold, fontSize = 17.sp)
        load?.let { Meter(it, Modifier.fillMaxWidth().padding(top = 8.dp)) }
    }
}

@Composable
fun ConfigTab(run: Run) {
    Column(verticalArrangement = ListGap) {
        flat(run.summary).takeIf { it.isNotEmpty() }?.let { Table("Summary", it) }
        Table("Config", flat(run.config))
        Table("Run", listOf("id" to run.id, "host" to run.host, "tags" to run.tags.joinToString(", "),
            "started" to DateFormat.getDateTimeInstance(DateFormat.MEDIUM, DateFormat.SHORT).format(Date(run.startedAt))).filter { it.second.isNotEmpty() } + flat(run.git, "git."))
    }
}

/** Keys in monospace, values as they were written. */
@Composable
private fun Table(title: String, rows: List<Pair<String, String>>) {
    SectionTitle(title)
    if (rows.isEmpty()) Empty("Nothing recorded.") else Panel(padding = 0) {
        rows.forEachIndexed { i, (k, v) ->
            if (i > 0) HorizontalDivider(color = T.rule)
            KeyValue(k, v)
        }
    }
}

/**
 * stdout and stderr in what is left of the screen, newest at the bottom. It follows the run while the reader is
 * at the end and lets go when they scroll up. A line that names a failure is in the failed colour, whatever
 * stream it came on: progress bars are written to stderr too.
 */
@Composable
fun LogsTab(store: Store, id: String, running: Boolean, modifier: Modifier = Modifier) {
    var lines by remember { mutableStateOf(listOf<LogLine>()) }
    var loaded by remember { mutableStateOf(false) }
    val state = rememberLazyListState()
    val bad = remember { Regex("error|exception|traceback", RegexOption.IGNORE_CASE) }
    Poll(id, { if (running) 3_000 else 60_000 }) {
        val path = lines.lastOrNull()?.let { "/api/v1/runs/$id/logs?after=${it.seq}&limit=2000" } ?: "/api/v1/runs/$id/logs?tail&limit=1000"
        runCatching { parseLogs(store.get(path)) }.getOrNull()?.let { next -> lines = (lines + next).takeLast(3000); loaded = true }
    }
    // Following is a decision the reader makes by scrolling, not where the list happens to be: the box also gets
    // shorter under them (an alert arrives in the header), and that must not leave the newest lines out of sight.
    var follow by remember { mutableStateOf(true) }
    var height by remember { mutableStateOf(0) }
    LaunchedEffect(state.isScrollInProgress) { if (!state.isScrollInProgress && lines.isNotEmpty()) follow = !state.canScrollForward }
    LaunchedEffect(lines.lastOrNull()?.seq, height) { if (follow && lines.isNotEmpty()) state.scrollToItem(lines.lastIndex) }
    Box(modifier.fillMaxWidth().onSizeChanged { height = it.height }.clip(RoundedCornerShape(8.dp)).background(T.panel).border(1.dp, T.rule, RoundedCornerShape(8.dp))) {
        if (loaded && lines.isEmpty()) Text("No output yet. What the script prints shows up here.", color = T.muted, fontSize = 14.sp, modifier = Modifier.padding(14.dp))
        SelectionContainer {
            LazyColumn(state = state, contentPadding = PaddingValues(12.dp), modifier = Modifier.fillMaxSize()) {
                items(lines, key = { it.seq }) { l ->
                    // Untrimmed: a line that wraps is spaced like the lines around it.
                    Text(l.line, fontSize = 12.sp, lineHeight = 17.sp, color = if (bad.containsMatchIn(l.line)) T.failed else T.ink,
                        style = Mono.copy(lineHeightStyle = LineHeightStyle(LineHeightStyle.Alignment.Center, LineHeightStyle.Trim.None)))
                }
            }
        }
    }
}

/** A slider through steps: a 4dp line and a round thumb. Material's own is a 16dp bar with a tall handle, louder than the images under it. */
@Composable
private fun Scrubber(index: Int, last: Int, modifier: Modifier = Modifier, pick: (Int) -> Unit) {
    Slider(
        value = index.toFloat(), valueRange = 0f..last.toFloat(), steps = (last - 1).coerceAtLeast(0), onValueChange = { pick(it.roundToInt()) }, modifier = modifier,
        thumb = { Box(Modifier.size(20.dp).clip(CircleShape).background(T.ink)) },
        track = { st ->
            SliderDefaults.Track(st, Modifier.height(4.dp), drawStopIndicator = null, drawTick = { _, _ -> }, thumbTrackGapSize = 0.dp, trackInsideCornerSize = 0.dp,
                colors = SliderDefaults.colors(activeTrackColor = T.ink, inactiveTrackColor = T.rule))
        },
    )
}

/**
 * Every image and video key on one scrubber, side by side, so each prompt is seen at the same step (DESIGN.md);
 * text tables below, a key and a slider each. Left at the end a slider follows the newest step. Tap an image to
 * look closely.
 */
@Composable
fun SamplesTab(store: Store, id: String, running: Boolean) {
    var media by remember { mutableStateOf(listOf<Media>()) }
    var loaded by remember { mutableStateOf(false) }
    var step by remember { mutableStateOf<Int?>(null) } // the grid's step; null follows the newest
    val picked = remember { mutableStateMapOf<String, Int>() } // a text key's place, the same way
    var viewing by remember { mutableStateOf<Pair<List<Media>, Int>?>(null) }
    Poll(id, { if (running) 10_000 else 120_000 }) {
        runCatching { media = parseMedia(store.get("/api/v1/runs/$id/media")) }
        loaded = true
    }
    val (texts, visual) = media.partition { it.kind == "text" }
    val steps = visual.map { it.step }.distinct().sorted()
    Column(verticalArrangement = Arrangement.spacedBy(24.dp)) {
        if (loaded && media.isEmpty()) Empty("No samples yet. What the script logs with run.image(), run.video() and run.text() shows up here.")
        if (steps.isNotEmpty()) Column {
            val index = steps.indexOf(step).takeIf { it >= 0 } ?: steps.lastIndex
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (steps.size > 1) Scrubber(index, steps.lastIndex, Modifier.weight(1f).padding(end = 12.dp)) { step = if (it >= steps.lastIndex) null else steps[it] }
                Text("step ${count(steps[index].toDouble())}" + if (steps.size > 1) " · ${index + 1} of ${steps.size}" else "", fontSize = 13.sp, color = T.muted, maxLines = 1)
            }
            val cells = samplesAt(visual, steps[index])
            val images = cells.filter { it.kind == "image" || it.url.endsWith(".gif") }
            // Two a row, so prompts can be compared; a lone sample takes the width, big enough to judge.
            Column(Modifier.padding(top = 4.dp), verticalArrangement = ListGap) {
                cells.chunked(if (cells.size == 1) 1 else 2).forEach { pair ->
                    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        pair.forEach { m ->
                            val open = if (m in images) Modifier.clickable(onClickLabel = "Look closely") { viewing = images to images.indexOf(m) } else Modifier
                            Column(Modifier.weight(1f).then(open)) {
                                if (m in images) RemoteImage(store, thumb(m.url), description = m.caption ?: m.key) // the viewer fetches the original
                                else Video(store, m.url)
                                // Which prompt this is, and what it was. The step is on the scrubber; a key that logged nothing there says where its image is from.
                                Text(buildAnnotatedString {
                                    append(m.key)
                                    if (m.step != steps[index]) withStyle(SpanStyle(color = T.muted, fontFamily = FontFamily.Default)) { append("  from step ${count(m.step.toDouble())}") }
                                }, fontSize = 12.sp, style = Mono, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp))
                                m.caption?.let { Text(it, fontSize = 13.sp, color = T.muted, maxLines = 3, overflow = TextOverflow.Ellipsis) }
                            }
                        }
                        if (pair.size < 2 && cells.size > 1) Spacer(Modifier.weight(1f))
                    }
                }
            }
        }
        texts.groupBy { it.key }.toSortedMap().forEach { (key, items) ->
            val at = items.map { it.step }.distinct().sorted()
            val index = minOf(picked[key] ?: at.lastIndex, at.lastIndex)
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(key, fontWeight = FontWeight.SemiBold, fontSize = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                    Text("step ${count(at[index].toDouble())}", fontSize = 13.sp, color = T.muted)
                }
                if (at.size > 1) Scrubber(index, at.lastIndex) { if (it >= at.lastIndex) picked.remove(key) else picked[key] = it }
                items.filter { it.step == at[index] }.forEach { TextTable(store, it.url) }
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
                    fontSize = 13.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
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

/** run.video() as a file the platform plays: tap to play, looping and muted, tap again to stop. */
@Composable
fun Video(store: Store, url: String) {
    var playing by remember(url) { mutableStateOf(false) }
    var ratio by remember(url) { mutableStateOf(16f / 9) }
    Box(Modifier.fillMaxWidth().aspectRatio(ratio).clip(RoundedCornerShape(8.dp)).background(T.soft).clickable { playing = !playing }, Alignment.Center) {
        if (playing) AndroidView({ c ->
            VideoView(c).apply {
                // The player fetches the file itself, so it is handed the session.
                setVideoURI(Uri.parse(if ("://" in url) url else store.server + url), store.session?.let { mapOf("Cookie" to "runyard_session=$it") }.orEmpty())
                setOnPreparedListener { it.isLooping = true; it.setVolume(0f, 0f); if (it.videoHeight > 0) ratio = it.videoWidth.toFloat() / it.videoHeight; start() }
                setOnErrorListener { _, _, _ -> playing = false; true } // no "Can't play this video" dialog over the app
            }
        }, Modifier.fillMaxSize())
        else Icon(Icons.Filled.PlayArrow, "Play the video", Modifier.size(48.dp), tint = T.muted)
    }
}

/** A GIF (run.video() where there is no ffmpeg), moving: the platform decodes and animates it. */
@Composable
private fun Gif(store: Store, url: String, modifier: Modifier) {
    var drawable by remember(url) { mutableStateOf<Drawable?>(null) }
    LaunchedEffect(url) {
        drawable = runCatching {
            val b = store.api.bytes(url)
            withContext(kotlinx.coroutines.Dispatchers.Default) { ImageDecoder.decodeDrawable(ImageDecoder.createSource(java.nio.ByteBuffer.wrap(b))) }
        }.getOrNull()
    }
    val d = drawable
    if (d == null) Box(modifier.aspectRatio(1f).clip(RoundedCornerShape(8.dp)).background(T.soft))
    else AndroidView({ ImageView(it) }, modifier.aspectRatio(d.intrinsicWidth.toFloat() / d.intrinsicHeight).clip(RoundedCornerShape(8.dp))) {
        it.setImageDrawable(d)
        (d as? AnimatedImageDrawable)?.start()
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

/**
 * An image fetched with the session. One whose source is under 256 pixels is scaled to the nearest pixel, so it
 * stays crisp. `square` crops it to a small thumbnail of the size the modifier gives.
 */
@Composable
fun RemoteImage(store: Store, url: String, max: Int = 1024, modifier: Modifier = Modifier.fillMaxWidth(), square: Boolean = false, description: String? = null) {
    if (url.endsWith(".gif")) return Gif(store, url, modifier)
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
    val shape = RoundedCornerShape(if (square) 6.dp else 8.dp)
    if (img == null) Box(modifier.aspectRatio(1f).clip(shape).background(T.soft))
    else Image(img, description, modifier.aspectRatio(if (square) 1f else img.width.toFloat() / img.height).clip(shape),
        contentScale = if (square) ContentScale.Crop else ContentScale.Fit,
        filterQuality = if (maxOf(img.width, img.height) < 256) FilterQuality.None else FilterQuality.Medium)
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
                    Text(k, fontSize = 13.sp, color = T.muted)
                    Text(row.opt(k).toString(), fontSize = 14.sp, modifier = Modifier.padding(bottom = 6.dp))
                }
            }
        }
    }
}
