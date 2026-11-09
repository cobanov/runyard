@file:OptIn(ExperimentalMaterial3Api::class)

package dev.cobanov.runyard

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.systemBarsPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.toMutableStateList
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        val store = Store.get(this)
        if (savedInstanceState == null) open(intent)
        scheduleNotify(this, store.notify, keep = true) // an app update drops the job
        setContent { RunyardTheme { Box(Modifier.fillMaxSize().background(T.paper)) { Root(store) } } }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        open(intent)
    }

    /** runyard://connect?url=<server address>, which the web UI shows as a QR, and runyard://run/<run id>, a notification's tap. */
    private fun open(intent: Intent?) {
        val link = intent?.data?.takeIf { it.scheme == "runyard" } ?: return
        val store = Store.get(this)
        when (link.host) {
            // A server on the list is gone back to as it was left (nothing to do when it is this one); a new one is asked about.
            "connect" -> link.getQueryParameter("url")?.trim()?.trimEnd('/')?.takeIf { it.isNotEmpty() }?.let { url ->
                store.servers.firstOrNull { url == it || url == it.substringAfter("://") }?.let(store::switch) ?: run { store.link = url }
            }
            "run" -> link.lastPathSegment?.takeIf { Regex("[A-Za-z0-9-]+").matches(it) }?.let { store.openRun = it } // it goes into a URL
        }
    }
}

/** Connect, then sign in if the server wants it, then the app. */
@Composable
fun Root(store: Store) {
    val error = store.loadError
    // A runyard://connect link: the demo has nothing to lose, the connect screen takes it as if typed, a connected app asks first.
    val link = store.link
    if (link != null && store.demo) LaunchedEffect(link) { store.leaveDemo() }
    else if (link != null && store.server.isNotEmpty()) SwitchDialog(store, link)
    when {
        store.demo -> Main(store)
        store.server.isEmpty() -> ConnectScreen(store)
        store.me == null && error != null -> Centered {
            var busy by remember { mutableStateOf(false) }
            val scope = rememberCoroutineScope()
            val retry: suspend () -> Unit = { busy = true; store.loadMe(); busy = false }
            val owner = LocalLifecycleOwner.current
            // A server that restarts comes back without a tap: after 2 s, then twice as long each time, up to 30 s.
            LaunchedEffect(owner) {
                owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
                    var wait = 2000L
                    while (true) { delay(wait); retry(); wait = minOf(wait * 2, 30_000) }
                }
            }
            Text("Can't reach the server", style = Title)
            Text("${store.server}\n$error", color = T.muted, fontSize = 14.sp, modifier = Modifier.padding(vertical = 12.dp))
            PrimaryButton("Try again", busy = busy) { scope.launch { retry() } }
            Text("It keeps trying on its own.", color = T.muted, fontSize = 13.sp, modifier = Modifier.padding(top = 8.dp))
            TextButton(onClick = store::leave) { Text("Change server", color = T.ink) }
        }
        store.me == null -> Centered {
            CircularProgressIndicator(color = T.muted, strokeWidth = 2.dp)
            LaunchedEffect(store.server) { store.loadMe() }
        }
        store.needsLogin -> LoginScreen(store)
        else -> key(store.server) { Main(store) } // another server's open run and tab are not this one's
    }
}

/** A runyard://connect link while another server is connected: ask, then go the way a typed address goes. */
@Composable
fun SwitchDialog(store: Store, address: String) {
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    AlertDialog(
        onDismissRequest = { store.link = null }, title = { Text("Switch server?") },
        text = { Text(error ?: "Connect to $address? ${store.server} stays on your list of servers.") },
        confirmButton = {
            TextButton(enabled = !busy, onClick = {
                busy = true
                scope.launch {
                    try { store.connect(address); store.link = null } catch (e: Exception) { error = e.message }
                    busy = false
                }
            }) { Text(if (busy) "Connecting…" else "Switch", color = T.ink) }
        },
        dismissButton = { TextButton(onClick = { store.link = null }) { Text("Cancel", color = T.ink) } },
        containerColor = T.panel, shape = RoundedCornerShape(8.dp),
    )
}

val Title = androidx.compose.ui.text.TextStyle(fontSize = 24.sp, fontWeight = FontWeight.Bold, letterSpacing = Tight)

@Composable
fun Centered(content: @Composable () -> Unit) {
    Column(
        Modifier.fillMaxSize().systemBarsPadding().imePadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally,
    ) { Column(Modifier.widthIn(max = 420.dp), horizontalAlignment = Alignment.CenterHorizontally) { content() } }
}

@Composable
fun PrimaryButton(text: String, enabled: Boolean = true, busy: Boolean = false, onClick: () -> Unit) {
    Button(
        onClick = onClick, enabled = enabled && !busy, modifier = Modifier.fillMaxWidth().height(44.dp),
        shape = RoundedCornerShape(6.dp),
        // Disabled it is still plainly the screen's one filled button, only fainter: never a pale pill that reads as a field.
        colors = ButtonDefaults.buttonColors(containerColor = T.ink, contentColor = T.paper, disabledContainerColor = T.ink.copy(alpha = 0.3f), disabledContentColor = T.paper),
    ) {
        if (busy) CircularProgressIndicator(Modifier.size(18.dp), color = T.paper, strokeWidth = 2.dp)
        else Text(text, fontWeight = FontWeight.Medium)
    }
}

@Composable
fun Field(value: String, onChange: (String) -> Unit, label: String, placeholder: String = "", password: Boolean = false,
          keyboard: KeyboardType = KeyboardType.Text, done: () -> Unit = {}) {
    OutlinedTextField(
        value, onChange, label = { Text(label) }, placeholder = { Text(placeholder, color = T.muted) }, singleLine = true,
        colors = OutlinedTextFieldDefaults.colors(unfocusedBorderColor = T.rule, focusedBorderColor = T.ink, unfocusedLabelColor = T.muted, focusedLabelColor = T.ink,
            cursorColor = T.ink, unfocusedContainerColor = T.panel, focusedContainerColor = T.panel),
        visualTransformation = if (password) PasswordVisualTransformation() else androidx.compose.ui.text.input.VisualTransformation.None,
        keyboardOptions = KeyboardOptions(keyboardType = keyboard, imeAction = ImeAction.Go, autoCorrectEnabled = false),
        keyboardActions = KeyboardActions(onGo = { done() }),
        shape = RoundedCornerShape(6.dp), modifier = Modifier.fillMaxWidth(),
    )
}

/** First launch: the server's address, the way Jellyfin asks for it, or the demo. */
@Composable
fun ConnectScreen(store: Store) {
    var address by rememberSaveable { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val connect = {
        busy = true
        error = null
        scope.launch {
            try { store.connect(address) } catch (e: Exception) { error = e.message }
            busy = false
        }
        Unit
    }
    LaunchedEffect(store.link) { store.link?.let { address = it; store.link = null; connect() } }
    Centered {
        LogoMark(Modifier.size(56.dp))
        Text("runyard", style = Title, modifier = Modifier.padding(top = 8.dp))
        Text("Your training runs, from your own server", color = T.muted, fontSize = 14.sp)
        Spacer(Modifier.height(32.dp))
        if (store.servers.isNotEmpty()) {
            Text("Your servers", color = T.muted, fontSize = 13.sp, modifier = Modifier.fillMaxWidth().padding(bottom = 8.dp))
            ServerList(store)
            Spacer(Modifier.height(24.dp))
        }
        Field(address, { address = it }, if (store.servers.isEmpty()) "Server address" else "Another server's address", keyboard = KeyboardType.Uri, done = connect)
        Text(
            error ?: "IP or host name, as in 192.168.1.10:8080",
            color = if (error == null) T.muted else T.failed, fontSize = 13.sp, modifier = Modifier.fillMaxWidth().padding(top = 6.dp, bottom = 16.dp),
        )
        PrimaryButton("Connect", enabled = address.isNotBlank(), busy = busy, onClick = connect)
        Spacer(Modifier.height(20.dp))
        TextButton(onClick = store::startDemo) { Text("Try the demo", color = T.ink, fontWeight = FontWeight.Medium) }
        Text("Made-up runs that keep training on your phone.", color = T.muted, fontSize = 13.sp)
    }
}

/** The server has RUNYARD_AUTH on: a user made with `runyard-server user add`. */
@Composable
fun LoginScreen(store: Store) {
    var name by rememberSaveable { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val login = {
        busy = true
        error = null
        scope.launch {
            try { store.login(name, password) } catch (e: Exception) { error = e.message }
            busy = false
        }
        Unit
    }
    Centered {
        Text("Sign in", style = Title)
        Text(store.server.substringAfter("://") + if (store.plainHttp) " · http, not encrypted" else "", color = T.muted, fontSize = 14.sp)
        Spacer(Modifier.height(24.dp))
        Field(name, { name = it }, "Name")
        Spacer(Modifier.height(8.dp))
        Field(password, { password = it }, "Password", password = true, keyboard = KeyboardType.Password, done = login)
        Text(error ?: "", color = T.failed, fontSize = 13.sp, modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp))
        PrimaryButton("Sign in", enabled = name.isNotEmpty() && password.isNotEmpty(), busy = busy, onClick = login)
        TextButton(onClick = store::leave) { Text("Change server", color = T.ink) }
    }
}

sealed interface Route {
    data class Project(val name: String) : Route
    /** `samples` opens the run on its samples: the tap on a running card's thumbnail. */
    data class RunR(val id: String, val samples: Boolean = false) : Route
}

@Composable
fun Main(store: Store) {
    var tab by rememberSaveable { mutableIntStateOf(0) }
    val stack = rememberSaveable(saver = listSaver(
        save = { it.map { r -> when (r) { is Route.Project -> "p" + r.name; is Route.RunR -> (if (r.samples) "s" else "r") + r.id } } },
        restore = { it.map { s -> if (s[0] == 'p') Route.Project(s.drop(1)) else Route.RunR(s.drop(1), s[0] == 's') }.toMutableStateList() },
    )) { mutableStateListOf<Route>() }
    // The alerts nobody dismissed: the tab's badge, and the first row of the home screen.
    var active by remember { mutableStateOf(listOf<Alert>()) }
    val badge: suspend () -> Unit = { runCatching { active = parseAlerts(store.get("/api/v1/alerts?active&limit=20")) } }
    Poll(Unit, { 15_000 }, badge)
    LaunchedEffect(store.openRun) {
        store.openRun?.let { if (stack.lastOrNull() != Route.RunR(it)) stack.add(Route.RunR(it)); store.openRun = null }
    }
    BackHandler(stack.isNotEmpty()) { stack.removeAt(stack.lastIndex) }
    val open: (Route) -> Unit = { stack.add(it) }
    val back: () -> Unit = { stack.removeAt(stack.lastIndex) }
    Scaffold(
        containerColor = T.paper,
        topBar = { if (store.demo) DemoBar(store) },
        bottomBar = {
            NavigationBar(containerColor = T.panel, tonalElevation = 0.dp) {
                listOf(Triple("Runs", Icons.AutoMirrored.Filled.List, 0), Triple("Alerts", Icons.Filled.Notifications, 1), Triple("Server", Icons.Filled.Settings, 2))
                    .forEach { (label, icon, i) ->
                        NavigationBarItem(
                            selected = tab == i, onClick = { tab = i; stack.clear() },
                            icon = {
                                BadgedBox(badge = {
                                    if (i == 1 && active.isNotEmpty()) Badge(containerColor = T.failed, contentColor = T.paper) { Text(if (active.size < 20) "${active.size}" else "20+") }
                                }) { Icon(icon, contentDescription = null) }
                            },
                            label = { Text(label) },
                            colors = NavigationBarItemDefaults.colors(indicatorColor = T.soft, selectedIconColor = T.ink, selectedTextColor = T.ink, unselectedIconColor = T.muted, unselectedTextColor = T.muted),
                        )
                    }
            }
        },
    ) { pad ->
        Box(Modifier.padding(pad).fillMaxSize()) {
            // The screens under the top one stay composed but are not placed (nothing drawn, nothing to touch):
            // going back finds a list where it was left.
            val levels = listOf<Route?>(null) + stack
            levels.forEachIndexed { i, r ->
                val top = i == levels.lastIndex
                key(i, r) {
                    CompositionLocalProvider(LocalShown provides top) {
                        Box(Modifier.fillMaxSize().layout { m, c -> m.measure(c).let { p -> layout(p.width, p.height) { if (top) p.place(0, 0) } } }) {
                            when (r) {
                                null -> when (tab) {
                                    0 -> HomeScreen(store, active, { tab = 1 }, open)
                                    1 -> AlertsScreen(store, open, badge)
                                    else -> ServerScreen(store)
                                }
                                is Route.Project -> RunsScreen(store, r.name, open, back)
                                is Route.RunR -> RunScreen(store, r.id, back, r.samples)
                            }
                        }
                    }
                }
            }
        }
    }
}

/** A slim bar while the demo runs, with the way out. */
@Composable
fun DemoBar(store: Store) {
    Row(
        Modifier.fillMaxWidth().background(T.soft).statusBarsPadding().padding(start = 16.dp, end = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text("Demo: made-up runs", fontSize = 13.sp, color = T.muted, modifier = Modifier.weight(1f))
        TextButton(onClick = store::leaveDemo, contentPadding = PaddingValues(horizontal = 12.dp)) {
            Text("Connect a server", fontSize = 13.sp, color = T.ink, fontWeight = FontWeight.Medium)
        }
    }
}

@Composable
fun ServerScreen(store: Store) {
    var machines by remember { mutableStateOf(listOf<Machine>()) }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val version = remember { context.packageManager.getPackageInfo(context.packageName, 0).versionName.orEmpty() }
    var denied by remember { mutableStateOf(false) }
    val notify: (Boolean) -> Unit = { store.notifications(it); scheduleNotify(context, it) }
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { ok -> denied = !ok; if (ok) notify(true) }
    val load: suspend () -> Unit = { runCatching { machines = parseMachines(store.get("/api/v1/hosts")) } }
    Poll(Unit, { 10_000 }, load)
    Screen("Server") {
        Refreshable(load) {
            LazyColumn(contentPadding = ListPadding, verticalArrangement = ListGap, modifier = Modifier.fillMaxSize()) {
                // Where the app is and as whom: the title already says "Server", so no heading says it again.
                item {
                    Panel {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Column(Modifier.weight(1f)) {
                                Text(if (store.demo) "The demo" else store.server, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                Text(
                                    if (store.demo) "made-up runs, inside the app"
                                    else listOfNotNull(if (store.plainHttp) "http, not encrypted" else null, store.me?.let { if (it.auth) "signed in as ${it.user}" else "open server" }).joinToString(" · "),
                                    fontSize = 13.sp, color = T.muted,
                                )
                            }
                            if (store.demo) TextButton(onClick = store::leaveDemo) { Text("Leave", color = T.ink) }
                            else if (store.me?.auth == true) TextButton(onClick = { scope.launch { store.logout() } }) { Text("Sign out", color = T.ink) }
                        }
                    }
                }
                if (!store.demo) {
                    item { SectionTitle("Servers") }
                    item { ServerList(store, add = store::leave) }
                    item { SectionTitle("Notifications") }
                    item {
                        Panel {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Text("When a run ends or an alert fires", modifier = Modifier.weight(1f).padding(end = 12.dp))
                                Switch(checked = store.notify, onCheckedChange = { on ->
                                    if (on && Build.VERSION.SDK_INT >= 33 && context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED)
                                        ask.launch(Manifest.permission.POST_NOTIFICATIONS)
                                    else notify(on)
                                }, colors = SwitchDefaults.colors(uncheckedTrackColor = T.soft, uncheckedBorderColor = T.rule, uncheckedThumbColor = T.muted))
                            }
                            Text(
                                if (denied) "Android does not let runyard notify. Allow it in the system settings, then turn this on."
                                else "Checked in the background, so they can arrive late. For instant ones, use ntfy or web push from the server.",
                                fontSize = 13.sp, color = if (denied) T.crashed else T.muted, modifier = Modifier.padding(top = 6.dp),
                            )
                        }
                    }
                }
                if (machines.isNotEmpty()) {
                    item { SectionTitle("Machines") }
                    items(machines, key = { it.name }) { MachineRow(it) }
                    item { Text("From runyard beacon on each machine: every process on its GPUs, whoever started it.", fontSize = 13.sp, color = T.muted) }
                }
                item { Text("runyard $version", fontSize = 13.sp, color = T.muted, modifier = Modifier.padding(top = 10.dp)) }
            }
        }
    }
}

/**
 * A machine: the CPU and RAM in a line, then each GPU with its load at the right, one neutral bar, and under it
 * what runs there, so "GPU 0" is said once. Amber only when the machine went silent; its last readings then fade.
 */
@Composable
fun MachineRow(m: Machine) {
    val small = @Composable { text: String -> Text(text, fontSize = 13.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis) }
    Panel {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(m.name, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (m.silent) Text("silent since ${clock(m.seenAt)}", fontSize = 13.sp, color = T.crashed, modifier = Modifier.padding(start = 8.dp))
        }
        Column(Modifier.alpha(if (m.silent) 0.5f else 1f)) {
            listOfNotNull(m.load.cpu?.let { "CPU ${pct(it)}" }, m.load.ramGB?.let { "RAM ${gb(it)}" }).takeIf { it.isNotEmpty() }?.let { small(it.joinToString(" · ")) }
            m.load.gpus.forEach { g ->
                Row(Modifier.padding(top = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text("GPU ${g.id}", fontSize = 13.sp)
                    Text(listOfNotNull(g.memGB?.let(::gb), g.tempC?.let { "${Math.round(it)} °C" }, g.powerW?.let { "${Math.round(it)} W" }).joinToString(" · "),
                        fontSize = 13.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 8.dp).weight(1f))
                    Text(g.util?.let(::pct) ?: "–", fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = if (g.util == null) T.muted else T.ink)
                }
                Meter(g.util ?: 0.0, Modifier.fillMaxWidth().padding(top = 6.dp, bottom = 4.dp))
                m.procs.filter { it.startsWith("GPU ${g.id} · ") }.forEach { small(it.removePrefix("GPU ${g.id} · ")) }
            }
            m.procs.filter { p -> m.load.gpus.none { p.startsWith("GPU ${it.id} · ") } }.forEach { small(it) }
        }
        if (m.runs.isNotEmpty()) Text(androidx.compose.ui.text.buildAnnotatedString {
            pushStyle(androidx.compose.ui.text.SpanStyle(color = T.running)); append("training"); pop()
            append(" " + m.runs.joinToString(", "))
        }, fontSize = 13.sp, color = T.muted, modifier = Modifier.padding(top = 10.dp))
    }
}

/** The runyard mark: two running-track lanes forming an r, the inside one in the running teal. */
@Composable
fun LogoMark(modifier: Modifier = Modifier) {
    val ink = T.ink
    val teal = T.running
    Canvas(modifier) {
        scale(size.minDimension / 256f, pivot = Offset.Zero) {
            drawPath(lane(108f, 72f), ink)
            drawPath(lane(48f, 12f), teal)
        }
    }
}

/** One lane in the logo's 256 box: centred on (144, 144), ends at 220, the right end cut at 45°. */
private fun lane(outer: Float, inner: Float) = Path().apply {
    val c = 144f
    val a = c - outer
    val b = c - inner
    moveTo(a, 220f)
    lineTo(a, c)
    arcTo(androidx.compose.ui.geometry.Rect(a, a, a + 2 * outer, a + 2 * outer), 180f, 90f, false)
    lineTo(220f, a)
    lineTo(220f - (outer - inner), b)
    lineTo(c, b)
    arcTo(androidx.compose.ui.geometry.Rect(b, b, b + 2 * inner, b + 2 * inner), 270f, -90f, false)
    lineTo(b, 220f)
    close()
}
