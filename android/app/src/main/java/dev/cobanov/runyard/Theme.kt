package dev.cobanov.runyard

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color

// DESIGN.md's tokens: a quiet, neutral dashboard where colour means state. web/src/app.css holds the values.
data class Tokens(
    val paper: Color, val panel: Color, val soft: Color, val rule: Color, val ink: Color, val muted: Color,
    val running: Color, val finished: Color, val failed: Color, val crashed: Color,
)

val LightTokens = Tokens(
    paper = Color(0xFFF9FAFB), panel = Color(0xFFFFFFFF), soft = Color(0xFFF3F4F6), rule = Color(0xFFE5E7EB),
    ink = Color(0xFF18181B), muted = Color(0xFF6B7280),
    running = Color(0xFF0F9488), finished = Color(0xFF52525B), failed = Color(0xFFDC2626), crashed = Color(0xFFD97706),
)

val DarkTokens = Tokens(
    paper = Color(0xFF09090B), panel = Color(0xFF121214), soft = Color(0xFF1C1C1F), rule = Color(0xFF27272A),
    ink = Color(0xFFFAFAFA), muted = Color(0xFFA1A1AA),
    running = Color(0xFF2DD4BF), finished = Color(0xFFA1A1AA), failed = Color(0xFFF87171), crashed = Color(0xFFFBBF24),
)

val LocalTokens = staticCompositionLocalOf { LightTokens }
val T: Tokens @Composable get() = LocalTokens.current

fun Tokens.status(s: String) = when (s) {
    "running" -> running
    "finished" -> finished
    "failed" -> failed
    "crashed" -> crashed
    else -> muted
}

fun Tokens.agent(state: String) = when (state) {
    "waiting" -> crashed
    "error" -> failed
    "done" -> finished
    "training", "working" -> running
    else -> muted
}

fun Tokens.level(l: String) = when (l) {
    "error" -> failed
    "warn" -> crashed
    else -> muted
}

@Composable
fun RunyardTheme(content: @Composable () -> Unit) {
    val t = if (isSystemInDarkTheme()) DarkTokens else LightTokens
    val base = if (isSystemInDarkTheme()) darkColorScheme() else lightColorScheme()
    // One filled primary, ink on paper; the running teal is state, so it is not the theme's primary.
    val scheme = base.copy(
        primary = t.ink, onPrimary = t.paper, primaryContainer = t.soft, onPrimaryContainer = t.ink,
        secondary = t.muted, onSecondary = t.paper, secondaryContainer = t.soft, onSecondaryContainer = t.ink,
        tertiary = t.running,
        background = t.paper, onBackground = t.ink, surface = t.paper, onSurface = t.ink,
        surfaceVariant = t.soft, onSurfaceVariant = t.muted, surfaceTint = t.paper,
        surfaceContainerLowest = t.panel, surfaceContainerLow = t.panel, surfaceContainer = t.panel,
        surfaceContainerHigh = t.panel, surfaceContainerHighest = t.soft, surfaceBright = t.panel, surfaceDim = t.paper,
        outline = t.rule, outlineVariant = t.rule, error = t.failed,
    )
    CompositionLocalProvider(LocalTokens provides t) { MaterialTheme(colorScheme = scheme, content = content) }
}
