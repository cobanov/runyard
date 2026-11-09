package dev.cobanov.runyard

import android.content.Context
import android.content.SharedPreferences
import android.net.http.HttpResponseCache
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.io.File

/** Which server the app talks to and who it is signed in as. The address and the session live in app-private prefs. */
class Store private constructor(private val prefs: SharedPreferences, private val samples: File) {
    var server by mutableStateOf(prefs.getString("server", "").orEmpty()); private set
    var session by mutableStateOf(prefs.getString("session", null)); private set
    var demo by mutableStateOf(false); private set
    /** null until /api/v1/me answered for the current server. */
    var me by mutableStateOf<Me?>(null)
    var loadError by mutableStateOf<String?>(null)
    /** An address from a runyard://connect link, until the connect screen or the switch dialog takes it. */
    var link by mutableStateOf<String?>(null)
    /** A run from a runyard://run link (a notification's tap), until the app opens it. */
    var openRun by mutableStateOf<String?>(null)
    /** The background check for ended runs and new alerts (Notify.kt) is on. */
    var notify by mutableStateOf(prefs.getBoolean("notify", false)); private set
    private var demoBackend: Demo? = null

    init { HttpResponseCache.install(samples, 1L shl 30) } // samples never change, so they stay on the device

    private fun dropSamples() {
        HttpResponseCache.getInstalled()?.delete() // this also uninstalls it
        HttpResponseCache.install(samples, 1L shl 30)
    }

    val api: Backend get() = demoBackend ?: Http(server, session)
    val needsLogin get() = me?.let { it.auth && it.user.isEmpty() } ?: false
    /** Without RUNYARD_AUTH the server deletes only for an API key, and the app has none. */
    val canDelete get() = demo || me?.auth == true
    val plainHttp get() = !demo && server.startsWith("http://")

    private fun save(server: String, session: String?) {
        val e = prefs.edit().putString("server", server).putString("session", session)
        if (server != this.server) e.remove("runs").remove("alert") // another server's runs are not news
        e.apply()
        this.server = server
        this.session = session
    }

    /** Finds a runyard server at what the user typed (`candidates`), and says why each try failed when none answers. */
    suspend fun connect(address: String) {
        val a = address.trim().trimEnd('/')
        if (a.isEmpty()) throw ApiError(0, "Enter the server's address.")
        val failed = mutableListOf<String>()
        for (base in candidates(a)) {
            val why = withContext(Dispatchers.IO) {
                try {
                    val (code, body, _) = Http(base, null).call("/api/v1/health", timeout = 8000)
                    if (code == 200 && runCatching { JSONObject(String(body)).optBoolean("ok") }.getOrDefault(false)) null
                    else "answered (HTTP $code) but is not a runyard server"
                } catch (e: Exception) {
                    reason(e)
                }
            }
            if (why == null) {
                me = null
                save(base, null)
                loadMe()
                return
            }
            failed += "$base: $why"
            if ("DNS" in why) break // the other schemes and ports would ask for the same name
        }
        throw ApiError(0, "No runyard server answered.\n" + failed.joinToString("\n"))
    }

    fun notifications(on: Boolean) {
        notify = on
        prefs.edit().putBoolean("notify", on).remove("runs").remove("alert").apply() // the first check after this is the baseline
    }

    /** The background check: asks the server, remembers what it saw, and returns what changed since last time. */
    suspend fun check(): List<Note> {
        if (server.isEmpty() || demo) return emptyList()
        val runs = parseRuns(api.get("/api/v1/runs?limit=50"))
        val alerts = parseAlerts(api.get("/api/v1/alerts?limit=20"))
        val before = prefs.getString("runs", null)?.let(::JSONObject)?.let { o -> o.keys().asSequence().associateWith(o::getString) }
        val seen = prefs.getInt("alert", 0)
        prefs.edit().putString("runs", JSONObject(runs.associate { it.id to it.status }).toString())
            .putInt("alert", maxOf(seen, alerts.maxOfOrNull { it.id } ?: 0)).apply()
        return news(before, seen, runs, alerts)
    }

    suspend fun loadMe() {
        try {
            me = parseMe(api.get("/api/v1/me"))
            loadError = null
        } catch (e: Exception) {
            loadError = reason(e)
        }
    }

    suspend fun login(name: String, password: String) {
        val body = JSONObject().put("name", name).put("password", password).toString()
        val (code, data, cookies) = withContext(Dispatchers.IO) {
            try {
                Http(server, null).call("/api/v1/login", "POST", body)
            } catch (e: Exception) {
                throw ApiError(0, e.message ?: "Can't reach the server.")
            }
        }
        if (code == 401) throw ApiError(0, "Wrong name or password.")
        if (code !in 200..299) throw ApiError(code, runCatching { JSONObject(String(data)).getString("error") }.getOrDefault("HTTP $code"))
        val value = cookies.flatMap { it.split(';') }.map { it.trim() }
            .firstOrNull { it.startsWith("runyard_session=") }?.removePrefix("runyard_session=")
            ?: throw ApiError(0, "The server did not start a session.")
        save(server, value)
        loadMe()
    }

    suspend fun logout() {
        runCatching { api.send("/api/v1/logout", "POST") }
        save(server, null)
        me = null
        dropSamples()
        loadMe()
    }

    fun forget() {
        save("", null)
        me = null
        loadError = null
        dropSamples()
    }

    /** The demo: made-up runs that keep training, with no server. */
    fun startDemo() {
        demoBackend = Demo()
        demo = true
        me = Me(false, "")
    }

    fun leaveDemo() {
        demoBackend = null
        demo = false
        me = null
        loadError = null
    }

    /** DELETE for the long-press actions. Only an admin may, so a 401 or 403 is answered with a sentence saying so. */
    suspend fun delete(path: String) {
        try {
            api.send(path, "DELETE")
        } catch (e: ApiError) {
            if (e.status == 401 || e.status == 403) throw ApiError(0, "Only an admin can delete. Sign in as one.")
            throw e
        }
    }

    /** GET for the screens: a 401 means the session ended, so the app goes back to the login form. */
    suspend fun get(path: String): String = try {
        api.get(path)
    } catch (e: ApiError) {
        if (e.status == 401) {
            save(server, null)
            me = Me(true, "")
        }
        throw e
    }

    companion object {
        @Volatile private var instance: Store? = null
        fun get(context: Context) = instance ?: synchronized(this) {
            instance ?: Store(context.applicationContext.getSharedPreferences("runyard", Context.MODE_PRIVATE), File(context.cacheDir, "samples")).also { instance = it }
        }
    }
}
