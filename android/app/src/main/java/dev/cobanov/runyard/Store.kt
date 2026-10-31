package dev.cobanov.runyard

import android.content.Context
import android.content.SharedPreferences
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject

/** Which server the app talks to and who it is signed in as. The address and the session live in app-private prefs. */
class Store private constructor(private val prefs: SharedPreferences) {
    var server by mutableStateOf(prefs.getString("server", "").orEmpty()); private set
    var session by mutableStateOf(prefs.getString("session", null)); private set
    var demo by mutableStateOf(false); private set
    /** null until /api/v1/me answered for the current server. */
    var me by mutableStateOf<Me?>(null)
    var loadError by mutableStateOf<String?>(null)
    private var demoBackend: Demo? = null

    val api: Backend get() = demoBackend ?: Http(server, session)
    val needsLogin get() = me?.let { it.auth && it.user.isEmpty() } ?: false

    private fun save(server: String, session: String?) {
        this.server = server
        this.session = session
        prefs.edit().putString("server", server).putString("session", session).apply()
    }

    /** Finds a runyard server at what the user typed: a full URL, or host[:port] tried over https, then http. */
    suspend fun connect(address: String) {
        val a = address.trim().trimEnd('/')
        if (a.isEmpty()) throw ApiError(0, "Enter the server's address.")
        for (base in if ("://" in a) listOf(a) else listOf("https://$a", "http://$a")) {
            val ok = withContext(Dispatchers.IO) {
                runCatching {
                    val (code, body, _) = Http(base, null).call("/api/v1/health", timeout = 8000)
                    code == 200 && JSONObject(String(body)).optBoolean("ok")
                }.getOrDefault(false)
            }
            if (!ok) continue
            me = null
            save(base, null)
            loadMe()
            return
        }
        throw ApiError(0, "No runyard server answered at $a.")
    }

    suspend fun loadMe() {
        try {
            me = parseMe(api.get("/api/v1/me"))
            loadError = null
        } catch (e: Exception) {
            loadError = e.message ?: e.toString()
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
        loadMe()
    }

    fun forget() {
        save("", null)
        me = null
        loadError = null
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
            instance ?: Store(context.applicationContext.getSharedPreferences("runyard", Context.MODE_PRIVATE)).also { instance = it }
        }
    }
}
