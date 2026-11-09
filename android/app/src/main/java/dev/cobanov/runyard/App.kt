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
import androidx.compose.material3.Switch
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
            "connect" -> link.getQueryParameter("url")?.trim()?.trimEnd('/')
                ?.takeIf { it.isNotEmpty() && it !in setOf(store.server, store.server.substringAfter("://")) }?.let { store.link = it }
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
            TextButton(onClick = store::forget) { Text("Change server") }
        }
        store.me == null -> Centered {
            CircularProgressIndicator(color = T.running)
            LaunchedEffect(store.server) { store.loadMe() }
        }
        store.needsLogin -> LoginScreen(store)
        else -> Main(store)
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
        text = { Text(error ?: "Connect to $address and leave ${store.server}?") },
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

val Title = androidx.compose.ui.text.TextStyle(fontSize = 22.sp, fontWeight = FontWeight.SemiBold)

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
        colors = ButtonDefaults.buttonColors(containerColor = T.ink, contentColor = T.paper),
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
        LogoMark(Modifier.size(72.dp))
        Text("runyard", fontSize = 34.sp, fontWeight = FontWeight.Bold, modifier = Modifier.padding(top = 8.dp))
        Text("Connect to your training server", color = T.muted)
        Spacer(Modifier.height(32.dp))
        Field(address, { address = it }, "Server address", "192.168.1.10:8080", keyboard = KeyboardType.Uri, done = connect)
        Text(
            error ?: "An IP address or host name. Without http:// the app tries https first, and without a port also 8080.",
            color = if (error == null) T.muted else T.failed, fontSize = 13.sp, modifier = Modifier.fillMaxWidth().padding(top = 6.dp, bottom = 16.dp),
        )
        PrimaryButton("Connect", enabled = address.isNotBlank(), busy = busy, onClick = connect)
        Spacer(Modifier.height(28.dp))
        HorizontalDivider(color = T.rule)
        Spacer(Modifier.height(20.dp))
        Text("No server yet?", color = T.muted, fontSize = 14.sp)
        OutlinedButton(
            onClick = store::startDemo, modifier = Modifier.fillMaxWidth().padding(top = 8.dp).height(44.dp), shape = RoundedCornerShape(6.dp),
        ) { Text("Try the demo", color = T.ink, fontWeight = FontWeight.Medium) }
        Text("Made-up runs that keep training on your phone.", color = T.muted, fontSize = 13.sp, modifier = Modifier.padding(top = 6.dp))
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
        Text("Sign in", fontSize = 34.sp, fontWeight = FontWeight.Bold)
        Text(store.server.substringAfter("://"), color = T.muted)
        if (store.plainHttp) Text("http, not encrypted", color = T.crashed, fontSize = 13.sp)
        Spacer(Modifier.height(24.dp))
        Field(name, { name = it }, "Name")
        Spacer(Modifier.height(8.dp))
        Field(password, { password = it }, "Password", password = true, keyboard = KeyboardType.Password, done = login)
        Text(error ?: "", color = T.failed, fontSize = 13.sp, modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp))
        PrimaryButton("Sign in", enabled = name.isNotEmpty() && password.isNotEmpty(), busy = busy, onClick = login)
        TextButton(onClick = store::forget) { Text("Change server", color = T.muted) }
    }
}

sealed interface Route {
    data class Project(val name: String) : Route
    data class RunR(val id: String) : Route
}

@Composable
fun Main(store: Store) {
    var tab by rememberSaveable { mutableIntStateOf(0) }
    val stack = rememberSaveable(saver = listSaver(
        save = { it.map { r -> when (r) { is Route.Project -> "p" + r.name; is Route.RunR -> "r" + r.id } } },
        restore = { it.map { s -> if (s[0] == 'p') Route.Project(s.drop(1)) else Route.RunR(s.drop(1)) }.toMutableStateList() },
    )) { mutableStateListOf<Route>() }
    var active by remember { mutableIntStateOf(0) }
    val badge: suspend () -> Unit = { runCatching { active = parseAlerts(store.get("/api/v1/alerts?active&limit=20")).size } }
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
                                    if (i == 1 && active > 0) Badge(containerColor = T.failed, contentColor = T.paper) { Text(if (active < 20) "$active" else "20+") }
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
                                    0 -> HomeScreen(store, open)
                                    1 -> AlertsScreen(store, open, badge)
                                    else -> ServerScreen(store)
                                }
                                is Route.Project -> RunsScreen(store, r.name, open, back)
                                is Route.RunR -> RunScreen(store, r.id, back)
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
        Dot(T.running)
        Text("Demo: made-up runs", fontSize = 13.sp, color = T.ink, modifier = Modifier.padding(start = 8.dp).weight(1f))
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
            LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.fillMaxSize()) {
                if (machines.isNotEmpty()) {
                    item { SectionTitle("Machines") }
                    items(machines, key = { it.name }) { MachineRow(it) }
                    item { Text("From runyard beacon on each machine: every process on its GPUs, whoever started it.", fontSize = 12.sp, color = T.muted) }
                }
                item { SectionTitle("Server") }
                item {
                    Panel {
                        KeyValue("Address", if (store.demo) "Demo, inside the app" else store.server)
                        if (store.plainHttp) KeyValue("Connection", "http, not encrypted")
                        store.me?.let { KeyValue("Signed in as", if (it.auth) it.user else "Open server, no sign-in") }
                        KeyValue("App version", version)
                    }
                }
                if (!store.demo) item {
                    Panel {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text("Notify me when a run ends or an alert fires", modifier = Modifier.weight(1f).padding(end = 12.dp))
                            Switch(checked = store.notify, onCheckedChange = { on ->
                                if (on && Build.VERSION.SDK_INT >= 33 && context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED)
                                    ask.launch(Manifest.permission.POST_NOTIFICATIONS)
                                else notify(on)
                            })
                        }
                        Text(
                            if (denied) "Android does not let runyard notify. Allow it in the system settings, then turn this on."
                            else "The app asks the server when Android lets it, about every 15 minutes and less often while the phone sleeps, " +
                                "so a notification can be that late. The server's own channels are instant: ntfy, Slack or Telegram.",
                            fontSize = 12.sp, color = if (denied) T.crashed else T.muted, modifier = Modifier.padding(top = 6.dp),
                        )
                    }
                }
                item {
                    Column {
                        if (store.me?.auth == true && !store.demo) TextButton(onClick = { scope.launch { store.logout() } }) { Text("Sign out", color = T.ink) }
                        if (store.demo) TextButton(onClick = store::leaveDemo) { Text("Leave the demo", color = T.ink) }
                        else TextButton(onClick = store::forget) { Text("Change server", color = T.failed) }
                    }
                }
            }
        }
    }
}

@Composable
fun MachineRow(m: Machine) {
    Panel {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(m.name, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
            if (m.silent) Text("silent since ${clock(m.seenAt)}", fontSize = 12.sp, color = T.crashed)
        }
        Spacer(Modifier.height(8.dp))
        MachineStrip(m.load, dim = m.silent)
        m.procs.forEach { Text(it, fontSize = 12.sp, color = T.muted, maxLines = 1, overflow = TextOverflow.Ellipsis, style = Num) }
        if (m.runs.isNotEmpty()) Text("Training " + m.runs.joinToString(", "), fontSize = 12.sp, color = T.running, modifier = Modifier.padding(top = 4.dp))
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
