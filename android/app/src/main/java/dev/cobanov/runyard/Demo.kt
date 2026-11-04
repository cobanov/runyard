package dev.cobanov.runyard

import android.graphics.Bitmap
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.math.BigDecimal
import java.math.MathContext
import java.time.Instant
import java.util.Locale
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.ceil
import kotlin.math.cos
import kotlin.math.exp
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min
import kotlin.math.pow
import kotlin.math.roundToInt
import kotlin.math.roundToLong
import kotlin.math.sin
import kotlin.math.sqrt

// The demo's server: runyard's REST API answered inside the app from made-up runs that keep training while the
// app is open, the same story as web/demo/mock.js. Nothing leaves the phone, and leaving the demo starts over.

private const val S = 1000L
private const val M = 60 * S
private const val H = 60 * M
private const val D = 24 * H

// Seeded noise: the history looks the same on every visit, and live steps come from the same functions.
private fun hash(n0: Int): Double {
    var n = (n0 xor 0x9e3779b9.toInt()) * 0x85ebca6b.toInt()
    n = (n xor (n ushr 13)) * 0xc2b2ae35.toInt()
    return ((n xor (n ushr 16)).toLong() and 0xffffffffL) / 4294967296.0
}
private fun u(seed: Int, i: Int) = hash(seed * 0x27d4eb2f + i) // 0..1
private fun u(seed: Int, i: Long) = u(seed, i.toInt())
private fun g(seed: Int, i: Int) = (u(seed, i) + u(seed + 1, i) + u(seed + 2, i) - 1.5) * 2 // about normal
private fun g(seed: Int, i: Long) = g(seed, i.toInt())
private fun within(r: ClosedFloatingPointRange<Double>, seed: Int, i: Long) = r.start + (r.endInclusive - r.start) * u(seed, i)
private fun hex(seed: Int, n: Int) = (0 until n).joinToString("") { (u(seed, it) * 16).toInt().toString(16) }
private fun uuid(seed: Int) = hex(seed, 32).let { "${it.take(8)}-${it.substring(8, 12)}-4${it.substring(13, 16)}-a${it.substring(17, 20)}-${it.substring(20)}" }
private fun p5(v: Double) = if (v.isFinite() && v != 0.0) BigDecimal(v).round(MathContext(5)).toDouble() else v
private fun iso(t: Long) = Instant.ofEpochMilli(t).toString()
private fun jnum(v: Double): Any = if (v.isFinite()) v else v.toString() // NaN and Infinity as strings, as the server sends them
private fun f(pattern: String, vararg a: Any) = String.format(Locale.ROOT, pattern, *a)
private fun dur(ms: Long) = (ms / M.toDouble()).roundToLong().let { if (it < 60) "${it}m" else "${it / 60}h ${it % 60}m" }

private data class Prof(
    val util: ClosedFloatingPointRange<Double>, val mem: Double, val temp: ClosedFloatingPointRange<Double>,
    val power: ClosedFloatingPointRange<Double>, val cpu: ClosedFloatingPointRange<Double> = 0.0..0.0, val ram: Double = 0.0,
)

private class Sum(var count: Int, var step: Int, var last: Double, var min: Double, var max: Double)
private class Line(val seq: Int, val ts: Long, val stream: Int, val text: String)
private class Sample(val id: Int, val key: String, val step: Int, val kind: String, val url: String, val caption: String?)

private class SimRun(
    val seed: Int, val project: String, val name: String, val host: String, val gpus: List<Int>, val start: Long,
    secPerStep: Double, val endStep: Int?, val fate: String, var nanAt: Int?, val model: Model, val prof: Prof,
    val proc: String, val user: String, val config: JSONObject, val tags: List<String>, val notes: String,
    branch: String, dirty: Boolean,
) {
    val id = uuid(seed)
    val spp = secPerStep * S
    val pid = 10000 + (u(seed, 99) * 50000).toInt()
    val git: JSONObject = JSONObject().put("branch", branch).put("commit", hex(seed + 7, 40)).put("dirty", dirty)
    var status = "running"
    var summary = JSONObject()
    var endedAt: Long? = null
    var beat = start
    var step = 0
    var sysT = start
    var seq = 0
    val series = LinkedHashMap<String, MutableList<Pair<Int, Double>>>()
    val sums = LinkedHashMap<String, Sum>()
    val lines = ArrayList<Line>()
    val media = ArrayList<Sample>()

    fun timeAt(s: Int) = start + (s * spp).toLong()
    fun stepAt(t: Long) = floor((t - start) / spp).toInt()
    fun endMs() = if (endedAt != null) beat else Long.MAX_VALUE

    fun put(key: String, step: Int, v: Double) {
        series.getOrPut(key) { ArrayList() }.add(step to v)
        val m = sums[key]
        if (m == null) sums[key] = Sum(1, step, v, v, v)
        else {
            m.count++; m.step = step; m.last = v
            if (v.isFinite()) {
                m.min = if (m.min.isFinite()) min(m.min, v) else v
                m.max = if (m.max.isFinite()) max(m.max, v) else v
            }
        }
    }

    fun line(t: Long, text: String, stream: Int = 1) {
        lines.add(Line(++seq, t, stream, text))
        if (lines.size > 3000) lines.subList(0, lines.size - 3000).clear()
    }
}

private abstract class Model(val every: Int) {
    open fun begin(r: SimRun, t: Long) {}
    abstract fun at(r: SimRun, s: Int, t: Long, demo: Demo)
    open fun nan(r: SimRun, s: Int, t: Long) {}
    open fun done(r: SimRun, s: Int, t: Long) {}
}

private fun cosine(s: Int, total: Int, peak: Double, warm: Int) =
    if (s < warm) peak * s / warm else peak * 0.5 * (1 + cos(PI * (s - warm) / (total - warm)))
private fun bar(x: Double) = "█".repeat((x * 20).roundToInt().coerceIn(0, 20)).padEnd(20, ' ')

private val prompts = listOf(
    "Explain why the sky is blue in two sentences.",
    "Write a haiku about a GPU that never sleeps.",
    "What is the capital of Australia, and why not Sydney?",
)
private val answers = listOf(
    listOf("The sky is blue blue because the the sky light is is blue.", "GPU GPU warm / fans fans fans spinning spinning / the the night", "The capital of Australia is Sydney Sydney, the capital."),
    listOf("Sunlight scatters off air molecules, and blue light scatters most, so the sky looks blue.", "Fans hum through the night / gradients fall like snow / loss creeps toward dawn", "Canberra. It was built as a compromise between Sydney and Melbourne."),
    listOf(
        "Air molecules scatter short wavelengths far more than long ones (Rayleigh scattering), so blue light reaches your eyes from every direction of the sky.",
        "Fans hum, never still / a thousand gradients fall / dawn finds the loss low",
        "Canberra. Sydney and Melbourne both wanted it, so in 1908 a new city was chosen between them.",
    ),
)

private class Sft(val plateau: Double, val lr: Double, val tps: Double, val total: Int, val epochs: Int = 2, val full: Boolean = false) : Model(10) {
    override fun begin(r: SimRun, t: Long) {
        r.line(t, "Loading Qwen/Qwen3-8B in bf16 on ${r.gpus.size} GPU${if (r.gpus.size > 1) "s" else ""}")
        r.line(t + 2 * S, if (full) "DeepSpeed ZeRO-3: 8,190,735,360 parameters, all trainable" else "trainable params: 43,646,976 || all params: 8,234,382,336 || trainable%: 0.5301")
        r.line(t + 3 * S, "***** Running training *****")
        r.line(t + 3 * S, "  Num examples = 207,865")
        r.line(t + 3 * S, "  Num Epochs = $epochs")
        r.line(t + 3 * S, f("  Total optimization steps = %,d", total))
        r.line(t + 4 * S, "UserWarning: torch.utils.checkpoint: the use_reentrant parameter should be passed explicitly.", 2)
    }

    override fun at(r: SimRun, s: Int, t: Long, demo: Demo) {
        val x = r.seed
        val creep = r.nanAt?.let { max(0.0, 1 - (it - s) / 150.0).pow(2) } ?: 0.0 // the loss climbs before it breaks
        val loss = p5(plateau + 1.35 * exp(-s / 420.0) + 0.045 * g(x, s) + creep * 2.5)
        val rate = cosine(s, total, lr, 100)
        val grad = p5(0.32 + 1.1 * exp(-s / 250.0) + 0.05 * abs(g(x + 5, s)) + creep * 9)
        val epoch = (s.toDouble() / total * epochs * 100).roundToInt() / 100.0
        r.put("train/loss", s, loss)
        r.put("train/learning_rate", s, p5(rate))
        r.put("train/grad_norm", s, grad)
        r.put("train/tokens_per_s", s, (tps + 70 * g(x + 6, s)).roundToInt().toDouble())
        r.line(t, f("{'loss': %.4f, 'grad_norm': %.3f, 'learning_rate': %.3e, 'epoch': %s}", loss, grad, rate, epoch))
        if (s % 100 == 0) r.line(t, f("%3d%%|%s| %d/%d [%s<%s, %.2fs/it]", 100 * s / total, bar(s.toDouble() / total), s, total, dur((s * r.spp).toLong()), dur(((total - s) * r.spp).toLong()), r.spp / S))
        if (s % 200 == 0) {
            val ev = p5(plateau + 0.07 + 1.15 * exp(-s / 520.0) + 0.008 * g(x + 7, s) + creep * 3)
            r.put("eval/loss", s, ev)
            r.line(t, f("{'eval_loss': %.4f, 'eval_runtime': %.1f, 'epoch': %s}", ev, 41 + u(x, s) * 3, epoch))
        }
        if (s % 1000 == 0) r.line(t, "Saving model checkpoint to /data/runs/${r.name}/checkpoint-$s")
        if (s % 500 == 0) r.media.add(Sample(demo.mediaId++, "generations", s, "text", "demo:gen/${min(2, s / 1500)}", null))
    }

    override fun nan(r: SimRun, s: Int, t: Long) {
        r.put("train/loss", s, Double.NaN)
        r.put("train/grad_norm", s, Double.POSITIVE_INFINITY)
        r.line(t, f("{'loss': nan, 'grad_norm': inf, 'learning_rate': %.3e}", cosine(s, total, lr, 100)))
        r.line(t + S, "Traceback (most recent call last):", 2)
        r.line(t + S, "  File \"/data/src/train_sft.py\", line 212, in <module>", 2)
        r.line(t + S, "    trainer.train()", 2)
        r.line(t + S, "ValueError: loss is NaN at step $s; stopping (runyard check: nan train/loss)", 2)
    }

    override fun done(r: SimRun, s: Int, t: Long) = r.line(t, "Training completed. Saving the final adapter to /data/runs/${r.name}/final")
}

private val scenes = listOf("a castle at dusk", "a pine forest at night", "a beach at noon", "a mountain lake")

private class Diffusion : Model(20) {
    override fun begin(r: SimRun, t: Long) {
        r.line(t, "DiT-S/2 at 32x32, 32.9M parameters, EMA 0.9999, batch 256")
        r.line(t + S, "dataset: 48,210 sprites from /data/pixel32 (4 classes)")
    }

    override fun at(r: SimRun, s: Int, t: Long, demo: Demo) {
        val x = r.seed
        val loss = p5(0.027 + 0.095 * exp(-s / 9000.0) + 0.0035 * g(x, s))
        r.put("train/loss", s, loss)
        r.put("train/lr", s, p5(if (s < 1000) 1e-4 * s / 1000 else 1e-4))
        if (s % 100 == 0) r.line(t, f("step %,d | loss %.4f | lr 1.00e-04 | %.1f it/s", s, loss, S / r.spp))
        if (s % 2000 == 0) {
            val fid = p5(17.5 + 165 * exp(-s / 21000.0) + 0.7 * g(x + 3, s))
            r.put("eval/fid", s, fid)
            scenes.forEachIndexed { i, scene -> r.media.add(Sample(demo.mediaId++, "samples", s, "image", "demo:sprite/${i * 13 + 5}/$s", scene)) }
            r.line(t, f("saved 4 samples at step %,d, FID %.1f", s, fid))
        }
        if (s % 10000 == 0) r.line(t, "checkpoint: /data/runs/${r.name}/ema-$s.pt")
    }
}

private class Cifar(val arch: String, val lr: Double, val epochs: Int, val accMax: Double) : Model(50) {
    val per = 400

    override fun begin(r: SimRun, t: Long) = r.line(t, "$arch on CIFAR-10, $epochs epochs, SGD lr $lr momentum 0.9, cosine")

    override fun at(r: SimRun, s: Int, t: Long, demo: Demo) {
        val x = r.seed
        val ep = s.toDouble() / per
        val rate = cosine(s, epochs * per, lr, per)
        val loss = p5(0.05 + 2.1 * exp(-ep / (7 * sqrt(0.1 / lr))) + 0.06 * abs(g(x, s)))
        r.put("train/loss", s, loss)
        r.put("train/lr", s, p5(rate))
        if (s % per == 0) {
            val acc = p5(min(0.995, accMax * (1 - exp(-ep / 9)) + 0.004 * g(x + 2, s)))
            val vloss = p5(0.17 + 1.9 * exp(-ep / 8) + 0.02 * g(x + 3, s))
            r.put("val/acc", s, acc)
            r.put("val/loss", s, vloss)
            r.line(t, f("Epoch %d/%d [%s] loss %.3f | val_loss %.3f val_acc %.4f | lr %.4f", s / per, epochs, bar(ep / epochs), loss, vloss, acc, rate))
        }
    }

    override fun nan(r: SimRun, s: Int, t: Long) {
        r.put("train/loss", s, Double.NaN)
        r.line(t, "Epoch ${ceil(s.toDouble() / per).toInt()}: loss nan", 2)
        r.line(t + S, "RuntimeError: loss is NaN; stopping (runyard check: nan train/loss)", 2)
    }

    override fun done(r: SimRun, s: Int, t: Long) = r.line(t, f("best val_acc %.4f; saved model.pt", r.sums["val/acc"]?.max ?: 0.0))
}

// ---- the machines ----

private class Card(val idle: Prof)
private class Extra(val proc: String, val user: String, val pid: Int, val prof: Prof)
private class Box(val name: String, val ram: Double, val gpus: List<Card>, val extra: Map<Int, Extra> = emptyMap(), val gone: Long? = null)

private val A100 = Card(Prof(0.0..0.0, 0.6, 31.0..34.0, 52.0..60.0))
private val RTX = Card(Prof(0.0..0.0, 0.4, 33.0..37.0, 18.0..26.0))
private val TITAN = Card(Prof(0.0..0.0, 0.3, 35.0..38.0, 15.0..20.0))
private val serve = Prof(12.0..58.0, 61.8, 44.0..55.0, 120.0..230.0)

private val sftProf = Prof(91.0..99.0, 58.3, 61.0..69.0, 330.0..395.0, 14.0..26.0, 46.0)
private val fullProf = Prof(94.0..100.0, 79.1, 66.0..74.0, 360.0..400.0, 18.0..30.0, 88.0)
private val ditProf = Prof(95.0..100.0, 14.2, 70.0..78.0, 380.0..440.0, 9.0..16.0, 12.0)
private val resProf = Prof(86.0..97.0, 9.7, 64.0..72.0, 300.0..380.0, 20.0..32.0, 8.0)

private class GpuNow(val util: Double, val mem: Double, val temp: Double, val power: Double)

private fun gpuLoad(p: Prof, seed: Int, i: Long) = GpuNow(
    min(100.0, within(p.util, seed + 20, i)).roundToInt().toDouble(),
    p5(p.mem + 0.05 * g(seed + 21, i)),
    (p.temp.start + (p.temp.endInclusive - p.temp.start) * (0.5 + 0.35 * sin(i / 40.0) + 0.15 * u(seed + 22, i))).roundToInt().toDouble(),
    within(p.power, seed + 23, i).roundToInt().toDouble(),
)

class Demo(private val t0: Long = System.currentTimeMillis()) : Backend {
    internal var mediaId = 1
    private val runs = LinkedHashMap<String, SimRun>()
    private val alerts = ArrayList<JSONObject>()
    private var alertId = 1
    private val boxes = listOf(
        Box("a100-node", 18.0, listOf(A100, A100, A100, A100), mapOf(3 to Extra("python serve.py --model qwen3-32b", "lee", 48211, serve))),
        Box("lab-4090", 9.0, listOf(RTX, RTX)),
        Box("macbook-pro", 21.0, emptyList()),
        Box("old-titan", 6.0, listOf(TITAN), gone = t0 - 3 * H - 12 * M),
    )

    // Alerts from checks, at fixed moments: some in the past, one a minute into the visit.
    private class Scripted(val at: Long, val run: String, val level: String, val title: String, val body: String, var done: Boolean = false)
    private val scripted = listOf(
        Scripted(t0 - 26 * H, "qwen3-8b-lora-r64", "info", "eval/loss below 1.15", "check: eval/loss < 1.15, at step 4,200"),
        Scripted(t0 - 41 * M, "dit-s-pixel32", "warn", "sys/gpu0.temp_c above 78", "check: GPU 0 at 79 °C for 5 minutes"),
        Scripted(t0 + 50 * S, "qwen3-8b-full-lr2e-5", "warn", "train/grad_norm above 4", "check: train/grad_norm > 4; the loss is climbing too"),
    )

    // Coding agent sessions, one per project; a card goes with its run. The done card's run ended long before its report.
    private class Session(val id: String, val label: String, val cwd: String, val metric: String, val state: String, val said: String, val report: String, val run: String, val at: Long)
    private val sessions = listOf(
        Session("codex:0199c2e4", "", "/data/src/pixel32-dit", "eval/fid", "training", "", "FID is down to 21 at 80k steps and still falling, so this run goes on to 200k.", "dit-s-pixel32", t0 - 9 * M),
        Session("claude:7e41b0c2", "rank sweep", "/data/src/qwen-lora", "eval/loss", "done", "", "Rank 64 lands 0.06 below rank 16 on eval/loss for 4x the adapter. Keeping rank 64.", "qwen3-8b-lora-r64", t0 - 12 * M),
        Session("claude:a03f9d16", "resnet depth", "/data/src/cifar", "val/acc", "waiting", "resnet50 is about to finish near 0.96 val/acc. OK to start resnet101 on GPU 1?", "", "resnet50-cosine", t0 - 3 * M),
    )

    private fun add(
        seed: Int, project: String, name: String, host: String, gpus: List<Int>, start: Long, secPerStep: Double, endStep: Int?,
        model: Model, prof: Prof, proc: String, user: String, config: JSONObject, tags: List<String>, notes: String = "",
        fate: String = "finished", nanAt: Int? = null, branch: String = "main", dirty: Boolean = false,
    ) = SimRun(seed, project, name, host, gpus, start, secPerStep, endStep, fate, nanAt, model, prof, proc, user, config, tags, notes, branch, dirty)
        .also { runs[it.id] = it; model.begin(it, start) }

    private fun cfg(vararg kv: Pair<String, Any>) = JSONObject().apply { kv.forEach { put(it.first, it.second) } }
    private fun qwen(vararg kv: Pair<String, Any>) =
        cfg("model" to "Qwen/Qwen3-8B", "dataset" to "ultrachat_200k", "seq_len" to 4096, "batch_size" to 64, "epochs" to 2, *kv)
    private fun lora(r: Int) = cfg("r" to r, "alpha" to 2 * r, "dropout" to 0.05, "target" to "all-linear")
    private fun res(vararg kv: Pair<String, Any>) =
        cfg("dataset" to "cifar10", "batch_size" to 128, "optimizer" to "sgd", "momentum" to 0.9, "weight_decay" to 5e-4, *kv)
    private fun dit(ema: Double) = cfg("model" to "DiT-S/2", "resolution" to 32, "patch" to 2, "ema" to ema, "batch_size" to 256, "lr" to 1e-4)

    // The story: runs from the last days, three still training, and what happens in the next minutes.
    init {
        add(11, "qwen-sft", "qwen3-8b-lora-r64", "a100-node", listOf(0), t0 - 27 * H, 1.9, 6000, Sft(1.05, 1e-4, 2950.0, 6000), sftProf.copy(mem = 66.9), "python train_sft.py", "ana",
            qwen("lora" to lora(64), "lr" to 1e-4), listOf("lora", "r64"), "Rank 64: eval/loss 0.06 below r16 for 4x the adapter.")
        add(12, "qwen-sft", "qwen3-8b-lora-r16-nopack", "a100-node", listOf(1), t0 - 2 * D - 5 * H, 1.4, 2010, Sft(1.13, 2e-4, 2400.0, 6000), sftProf, "python train_sft.py", "kai",
            qwen("lora" to lora(16), "lr" to 2e-4, "packing" to false), listOf("lora", "r16"), "Node rebooted for a driver update mid-run.", fate = "crashed")
        add(13, "qwen-sft", "qwen3-8b-lora-r16", "a100-node", listOf(0), t0 - 52 * M, 1.1, 6000, Sft(1.12, 2e-4, 3410.0, 6000), sftProf, "python train_sft.py", "ana",
            qwen("lora" to lora(16), "lr" to 2e-4, "packing" to true), listOf("lora", "r16", "baseline"), "Baseline LoRA, rank 16 on every linear layer.")
        val full = add(14, "qwen-sft", "qwen3-8b-full-lr2e-5", "a100-node", listOf(1, 2), t0 - 38 * M, 2.3, 6000, Sft(1.0, 2e-5, 1840.0, 6000, full = true), fullProf, "torchrun train_sft.py --deepspeed zero3", "kai",
            qwen("lr" to 2e-5, "deepspeed" to "zero3", "full_finetune" to true), listOf("full"))
        full.nanAt = (ceil(full.stepAt(t0 + 150 * S) / 10.0) * 10).toInt() // breaks two and a half minutes in

        add(21, "pixel-diffusion", "unet-baseline", "lab-4090", listOf(0), t0 - 4 * D, 0.21, 60000, Diffusion(), ditProf.copy(mem = 11.8), "python train_unet.py", "ana",
            cfg("model" to "UNet", "channels" to JSONArray(listOf(64, 128, 256)), "resolution" to 32, "batch_size" to 256, "lr" to 1e-4), listOf("baseline"))
        add(22, "pixel-diffusion", "dit-s-pixel32-ema0.999", "lab-4090", listOf(0), t0 - 30 * H, 0.16, 80000, Diffusion(), ditProf, "python train_dit.py", "ana", dit(0.999), listOf("dit"))
        add(23, "pixel-diffusion", "dit-s-pixel32", "lab-4090", listOf(0), t0 - 3 * H - 20 * M, 0.15, 200000, Diffusion(), ditProf, "python train_dit.py", "ana", dit(0.9999), listOf("dit"),
            "EMA 0.9999; samples every 2,000 steps.", branch = "dit", dirty = true)

        add(31, "cifar10-resnet", "resnet18-lr0.1", "lab-4090", listOf(1), t0 - 3 * D, 0.045, 40000, Cifar("ResNet-18", 0.1, 100, 0.953), resProf, "python train.py", "kai",
            res("arch" to "resnet18", "lr" to 0.1, "epochs" to 100), listOf("sweep-lr"))
        add(32, "cifar10-resnet", "resnet18-lr0.05", "lab-4090", listOf(1), t0 - 3 * D + 40 * M, 0.045, 40000, Cifar("ResNet-18", 0.05, 100, 0.947), resProf, "python train.py", "kai",
            res("arch" to "resnet18", "lr" to 0.05, "epochs" to 100), listOf("sweep-lr"))
        add(33, "cifar10-resnet", "resnet18-lr0.4", "lab-4090", listOf(1), t0 - 2 * D - 20 * H, 0.045, 40000, Cifar("ResNet-18", 0.4, 100, 0.9), resProf, "python train.py", "kai",
            res("arch" to "resnet18", "lr" to 0.4, "epochs" to 100), listOf("sweep-lr"), nanAt = 2350)
        // Finishes about 75 seconds into the visit.
        add(34, "cifar10-resnet", "resnet50-cosine", "lab-4090", listOf(1), t0 + 75 * S - (200 * 400 * 0.06 * S).toLong(), 0.06, 80000, Cifar("ResNet-50", 0.1, 200, 0.962), resProf, "python train.py --arch resnet50", "kai",
            res("arch" to "resnet50", "lr" to 0.1, "epochs" to 200, "schedule" to "cosine"), listOf("resnet50"))
        tick(t0)
    }

    private fun alert(r: SimRun, t: Long, level: String, title: String, body: String = "") {
        alerts.add(0, JSONObject().put("id", alertId++).put("run", r.id).put("ts", iso(t)).put("level", level).put("title", title).put("body", body).put("dismissed_at", JSONObject.NULL))
    }

    private fun end(r: SimRun, status: String, t: Long) {
        r.status = status
        r.beat = t
        r.endedAt = if (status == "crashed") t + 5 * M else t // crashed: noticed five minutes after the last heartbeat
        r.summary = JSONObject().apply { r.sums.filterKeys { !it.startsWith("sys/") }.forEach { (k, m) -> if (m.last.isFinite()) put(k, m.last) } }
    }

    // advance plays a run's steps and system readings up to time t.
    private fun advance(r: SimRun, t: Long) {
        if (r.status != "running") return
        val m = r.model
        val target = min(r.stepAt(t), r.endStep ?: Int.MAX_VALUE)
        while (r.step + m.every <= target) {
            r.step += m.every
            val s = r.step
            val ts = r.timeAt(s)
            while (r.sysT + 10 * S <= ts) { r.sysT += 10 * S; sys(r, r.sysT) }
            m.at(r, s, ts, this)
            val nanAt = r.nanAt
            if (nanAt != null && s >= nanAt) {
                m.nan(r, s, ts)
                alert(r, ts, "error", "loss is NaN", f("train/loss NaN at step %,d: stopping.\nLower the learning rate or clip gradients.", s))
                return end(r, "failed", ts + 2 * S)
            }
            if (r.endStep != null && s >= r.endStep) {
                if (r.fate == "finished") m.done(r, s, ts)
                return end(r, r.fate, ts)
            }
        }
        while (r.sysT + 10 * S <= t) { r.sysT += 10 * S; sys(r, r.sysT) }
        r.beat = min(t, System.currentTimeMillis())
    }

    // sys logs what runyard's SDK samples every 10 s: CPU, memory, and each of the run's GPUs.
    private fun sys(r: SimRun, t: Long) {
        val e = ((t - r.start) / S).toInt()
        val i = t / (10 * S)
        val p = r.prof
        r.put("sys/cpu", e, p5(within(p.cpu, r.seed + 10, i)))
        r.put("sys/ram_gb", e, p5(p.ram + 0.4 * g(r.seed + 11, i)))
        for (gi in r.gpus) {
            val v = gpuLoad(p, r.seed + gi, i)
            r.put("sys/gpu$gi.util", e, v.util)
            r.put("sys/gpu$gi.mem_gb", e, v.mem)
            r.put("sys/gpu$gi.temp_c", e, v.temp)
            r.put("sys/gpu$gi.power_w", e, v.power)
        }
    }

    private fun tick(now: Long) {
        runs.values.forEach { advance(it, now) }
        for (a in scripted) if (!a.done && a.at <= now) {
            a.done = true
            runs.values.find { it.name == a.run }?.let { alert(it, a.at, a.level, a.title, a.body) }
        }
    }

    // load is what a machine's beacon reads at time t: whatever runs on each GPU then, or the idle card.
    private fun hostOut(b: Box, now: Long): JSONObject {
        val t = min(b.gone ?: Long.MAX_VALUE, now / (10 * S) * 10 * S)
        val i = t / (10 * S)
        val on = runs.values.filter { it.host == b.name && it.start <= t && t <= it.endMs() }
        val x = b.name.length * 101
        var cpu = 3 + 4 * u(x, i)
        var ram = b.ram + 0.3 * g(x + 1, i)
        val gpus = JSONArray()
        b.gpus.forEachIndexed { gi, card ->
            val r = on.find { gi in it.gpus }
            val extra = b.extra[gi]
            val p = r?.prof ?: extra?.prof ?: card.idle
            val v = gpuLoad(p, (r?.seed ?: x) + gi, i)
            val procs = JSONArray()
            if (r != null) procs.put(JSONObject().put("pid", r.pid + r.gpus.indexOf(gi)).put("name", r.proc).put("user", r.user).put("mem_gb", p5(v.mem - 0.4)))
            else if (extra != null) procs.put(JSONObject().put("pid", extra.pid).put("name", extra.proc).put("user", extra.user).put("mem_gb", p5(v.mem - 0.4)))
            gpus.put(JSONObject().put("i", gi).put("util", v.util).put("mem_gb", v.mem).put("temp_c", v.temp).put("power_w", v.power).put("procs", procs))
        }
        for (r in on) {
            cpu += within(r.prof.cpu, r.seed + 10, i) / (if (r.gpus.isNotEmpty()) 2 else 1)
            ram += r.prof.ram
        }
        if (b.extra.isNotEmpty()) { cpu += 6; ram += 24 }
        return JSONObject()
            .put("name", b.name).put("seen_at", iso(t)).put("silent", now - t > 2 * M)
            .put("stats", JSONObject().put("cpu", p5(min(100.0, cpu))).put("ram_gb", p5(ram)).put("gpus", gpus))
            .put("runs", JSONArray(runs.values.filter { it.host == b.name && it.status == "running" }.map {
                JSONObject().put("id", it.id).put("name", it.name).put("project", it.project)
            }))
    }

    private fun pub(r: SimRun, withMetrics: Boolean) = JSONObject()
        .put("id", r.id).put("project", r.project).put("name", r.name).put("status", r.status)
        .put("config", r.config).put("summary", r.summary).put("tags", JSONArray(r.tags)).put("notes", r.notes)
        .put("host", r.host).put("git", r.git).put("started_at", iso(r.start))
        .put("ended_at", r.endedAt?.let(::iso) ?: JSONObject.NULL).put("last_heartbeat_at", iso(r.beat))
        .apply {
            if (withMetrics) put("metrics", JSONObject().apply {
                r.sums.forEach { (k, m) ->
                    put(k, JSONObject().put("count", m.count).put("step", m.step).put("last", jnum(m.last)).put("min", jnum(m.min)).put("max", jnum(m.max)))
                }
            })
        }

    // series cuts a key's history into at most n buckets of equal step width, as the server does.
    private fun series(rows: List<Pair<Int, Double>>, n: Int): JSONObject {
        val step = ArrayList<Int>(); val lo = ArrayList<Double>(); val hi = ArrayList<Double>(); val last = ArrayList<Double>()
        val width = if (rows.size <= n) 0 else ceil((rows.last().first - rows.first().first + 1).toDouble() / n).toInt()
        var b = -1
        for ((s, v) in rows) {
            val k = if (width == 0) b + 1 else (s - rows.first().first) / width
            if (k != b) { b = k; step.add(s); lo.add(v); hi.add(v); last.add(v); continue }
            val i = step.size - 1
            step[i] = s; last[i] = v
            if (v.isFinite()) {
                lo[i] = if (lo[i].isFinite()) min(lo[i], v) else v
                hi[i] = if (hi[i].isFinite()) max(hi[i], v) else v
            }
        }
        return JSONObject().put("step", JSONArray(step)).put("min", JSONArray(lo.map(::jnum)))
            .put("max", JSONArray(hi.map(::jnum))).put("last", JSONArray(last.map(::jnum)))
    }

    private fun route(method: String, full: String, body: JSONObject): Any {
        val now = System.currentTimeMillis()
        tick(now)
        val path = full.substringBefore('?').removePrefix("/api/v1")
        val query = full.substringAfter('?', "").split('&').filter { it.isNotEmpty() }
            .associate { java.net.URLDecoder.decode(it.substringBefore('='), "UTF-8") to java.net.URLDecoder.decode(it.substringAfter('=', ""), "UTF-8") }
        val p = path.split('/').filter { it.isNotEmpty() }.map { java.net.URLDecoder.decode(it, "UTF-8") }
        val run = if (p.getOrNull(0) == "runs" && p.size > 1) runs[p[1]] else null
        val notFound = ApiError(404, "not found")

        if (full.startsWith("demo:gen/")) {
            val qi = full.removePrefix("demo:gen/").toInt()
            return JSONArray(prompts.indices.map { JSONObject().put("prompt", prompts[it]).put("response", answers[qi][it]) })
        }
        when (method) {
            "GET" -> {
                when {
                    path == "/health" -> return JSONObject().put("ok", true)
                    path == "/me" -> return JSONObject().put("auth", false).put("user", "")
                    path == "/projects" -> return JSONArray(runs.values.groupBy { it.project }.map { (name, rs) ->
                        JSONObject().put("name", name).put("runs", rs.size).put("running", rs.count { it.status == "running" })
                            .put("last_run_at", iso(rs.maxOf { it.start }))
                    }.sortedByDescending { it.getString("last_run_at") })
                    path == "/runs" -> {
                        var rs = runs.values.sortedByDescending { it.start }
                        query["project"]?.let { pr -> rs = rs.filter { it.project == pr } }
                        query["status"]?.let { st -> rs = rs.filter { it.status == st } }
                        return JSONArray(rs.take(query["limit"]?.toIntOrNull() ?: 100).map { pub(it, query["metrics"] == "1") })
                    }
                    run != null && p.size == 2 -> return pub(run, true)
                    run != null && p[2] == "metrics" -> {
                        val want = query["keys"]?.split(',')?.filter { it.isNotEmpty() }
                        val n = min(query["points"]?.toIntOrNull() ?: 600, 2000)
                        return JSONObject().apply { run.series.forEach { (k, rows) -> if (want == null || k in want) put(k, series(rows, n)) } }
                    }
                    run != null && p[2] == "logs" -> {
                        val limit = query["limit"]?.toIntOrNull() ?: 1000
                        val after = query["after"]?.toIntOrNull() ?: 0
                        val ls = if ("tail" in query) run.lines.takeLast(limit) else run.lines.filter { it.seq > after }.take(limit)
                        return JSONArray(ls.map { JSONObject().put("seq", it.seq).put("ts", iso(it.ts)).put("stream", it.stream).put("line", it.text) })
                    }
                    run != null && p[2] == "media" -> return JSONArray(run.media.map {
                        JSONObject().put("id", it.id.toString()).put("key", it.key).put("step", it.step).put("kind", it.kind).put("url", it.url)
                            .put("meta", JSONObject().apply { it.caption?.let { c -> put("caption", c) } })
                    })
                    run != null && p[2] == "artifacts" -> return JSONArray()
                    path == "/alerts" -> {
                        var out = alerts.mapNotNull { a -> runs[a.getString("run")]?.let { r -> JSONObject(a.toString()).put("run_name", r.name).put("project", r.project) } }
                        query["run"]?.let { id -> out = out.filter { it.getString("run") == id } }
                        return JSONArray(out.take(query["limit"]?.toIntOrNull() ?: 100))
                    }
                    path == "/hosts" -> return JSONArray(boxes.map { hostOut(it, now) })
                    path == "/agents" -> return JSONArray(sessions.mapNotNull { s ->
                        runs.values.find { it.name == s.run }?.let { r ->
                            JSONObject().put("id", s.id).put("tool", s.id.substringBefore(':')).put("label", s.label).put("host", r.host).put("cwd", s.cwd)
                                .put("project", r.project).put("metric", s.metric).put("state", s.state).put("said", s.said).put("report", s.report)
                                .put("runs", runs.values.count { it.project == r.project }).put("last_run", JSONObject().put("id", r.id).put("name", r.name))
                                .put("active_at", iso(max(s.at, r.beat)))
                        }
                    })
                }
                throw notFound
            }
            "POST" -> when (path) {
                "/alerts/dismiss" -> {
                    val ids = body.optJSONArray("ids")?.let { a -> (0 until a.length()).map { a.getInt(it) } }.orEmpty()
                    alerts.filter { it.isNull("dismissed_at") && (ids.isEmpty() || it.getInt("id") in ids) }.forEach { it.put("dismissed_at", iso(now)) }
                    return JSONObject()
                }
                "/logout" -> return JSONObject()
            }
            "DELETE" -> {
                if (run != null) { runs.remove(run.id); return JSONObject() }
                if (p.getOrNull(0) == "projects" && p.size > 1) { runs.values.removeAll { it.project == p[1] }; return JSONObject() }
            }
        }
        throw notFound
    }

    private suspend fun answer(method: String, path: String, json: String?): String {
        delay(30 + (Math.random() * 90).toLong()) // about a network's worth
        return withContext(Dispatchers.Default) {
            synchronized(this@Demo) { route(method, path, json?.let(::JSONObject) ?: JSONObject()).toString() }
        }
    }

    override suspend fun get(path: String) = answer("GET", path, null)
    override suspend fun send(path: String, method: String, json: String?) = answer(method, path, json)

    /** demo:sprite/<seed>/<step>: a 32x32 sample of a scene the way a diffusion model gets there, as a PNG. */
    override suspend fun bytes(url: String): ByteArray = withContext(Dispatchers.Default) {
        val (seed, step) = url.removePrefix("demo:sprite/").split('/').map { it.toInt() }
        val bmp = Bitmap.createBitmap(sprite(seed, step), 32, 32, Bitmap.Config.ARGB_8888)
        ByteArrayOutputStream().also { bmp.compress(Bitmap.CompressFormat.PNG, 100, it) }.toByteArray()
    }
}

// sprite draws coarse colour blobs under noise early in training, then finer detail as the noise fades.
private val skies = listOf(
    listOf(intArrayOf(40, 30, 90), intArrayOf(240, 120, 90)),
    listOf(intArrayOf(10, 14, 40), intArrayOf(40, 60, 110)),
    listOf(intArrayOf(90, 170, 240), intArrayOf(200, 230, 250)),
    listOf(intArrayOf(120, 160, 220), intArrayOf(230, 200, 170)),
)

private fun scene(kind: Int, seed: Int, x: Int, y: Int): DoubleArray {
    val n = 32
    val (top, low) = skies[kind]
    val sun = intArrayOf(8 + seed % 16, 7 + seed % 5)
    var px = DoubleArray(3) { top[it] + (low[it] - top[it]) * y.toDouble() / n }
    fun c(r: Int, g: Int, b: Int) = doubleArrayOf(r.toDouble(), g.toDouble(), b.toDouble())
    if ((x - sun[0]) * (x - sun[0]) + (y - sun[1]) * (y - sun[1]) < 10) px = if (kind == 1) c(230, 230, 210) else c(255, 220, 120)
    if (kind == 1 && u(seed, x * 97 + y) > 0.97 && y < 18) px = c(220, 220, 255)
    val hill = 19 + 3 * sin(x / 4.0 + seed)
    val front = 25 + 2 * sin(x / 2.5 + seed * 2)
    if (y > hill) px = if (kind == 2) c(230, 210, 150) else c(40, 90 + kind * 10, 60)
    if (y > front) px = if (kind == 2) c(40, 120, 200) else if (kind == 3) c(50, 90, 160) else c(30, 60, 40)
    if (kind == 0 && x in 20..27 && y >= 11 && y <= hill && !(y < 14 && x % 2 == 1)) px = c(70, 60, 80)
    if (kind == 1 && x % 7 == 3 && y > hill - 7 && y <= hill + 2) px = c(20, 50, 30)
    return px
}

private fun sprite(seed: Int, step: Int): IntArray {
    val n = 32
    val learned = 1 - exp(-step / 9000.0)
    val block = if (learned < 0.25) 8 else if (learned < 0.5) 4 else if (learned < 0.75) 2 else 1
    val amp = 255 * 0.9 * (1 - learned)
    val kind = (seed - 5) / 13
    return IntArray(n * n) { o ->
        val y = o / n
        val x = o % n
        val px = scene(kind, seed, x - x % block, y - y % block)
        val c = IntArray(3) { i -> (px[i] + amp * (u(seed * 3 + i + step, x * n + y) - 0.5)).roundToInt().coerceIn(0, 255) }
        (0xff shl 24) or (c[0] shl 16) or (c[1] shl 8) or c[2]
    }
}
