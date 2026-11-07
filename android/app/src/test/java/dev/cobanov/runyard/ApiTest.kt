package dev.cobanov.runyard

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ApiTest {
    // GET /api/v1/runs?metrics=1, as the Go server sends it: nine fraction digits, NaN as a string, nested config.
    private val runs = """
        [{"id":"7f3a","project":"qwen-sft","name":"qwen3-8b-full-lr2e-5","status":"failed",
          "config":{"model":"Qwen/Qwen3-8B","lora":{"r":16,"alpha":32},"lr":2e-5},"summary":{"train/loss":1.31},
          "tags":["full"],"notes":"","host":"a100-node","git":{"branch":"main","dirty":false},
          "started_at":"2026-10-31T09:12:03.123456789Z","ended_at":"2026-10-31T09:52:05.5+03:00",
          "last_heartbeat_at":null,
          "metrics":{
            "train/loss":{"count":240,"step":2400,"last":"NaN","min":1.05,"max":2.4},
            "eval/loss":{"count":12,"step":2400,"last":1.2,"min":1.2,"max":2.1},
            "train/grad_norm":{"count":240,"step":2400,"last":"Infinity","min":0.3,"max":9},
            "sys/gpu1.util":{"count":90,"step":900,"last":97,"min":0,"max":100},
            "sys/gpu0.temp_c":{"count":90,"step":900,"last":66,"min":40,"max":70},
            "sys/cpu":{"count":90,"step":900,"last":21.5,"min":3,"max":30}}}]
    """.trimIndent()

    private fun progress(config: String, metrics: String) = parseRuns(
        """[{"id":"r","status":"running","started_at":"2026-10-31T10:00:00Z","config":$config,"metrics":$metrics}]""").single().progress

    @Test fun progressFromSteps() {
        val m = """{"loss/loss":{"count":1,"step":300,"last":0.07,"min":0,"max":1},"sys/cpu":{"count":1,"step":900,"last":40,"min":0,"max":90}}"""
        // ai-toolkit: train.steps, with a sampler's steps beside it.
        assertEquals(Run.Progress(300.0, 6000.0, "step"), progress("""{"train":{"steps":6000},"sample":{"steps":28,"sample_steps":28}}""", m))
        assertEquals(Run.Progress(300.0, 500.0, "step"), progress("""{"trainer":{"max_steps":500,"num_train_epochs":3}}""", m))
        assertNull(progress("""{"sampler":{"steps":28}}""", m)) // past it: not the run's total
        assertNull(progress("""{"lr":0.001}""", m))
    }

    @Test fun progressFromEpochs() {
        val m = """{"train/epoch":{"count":1,"step":40,"last":1.5,"min":0,"max":2}}"""
        assertEquals(Run.Progress(1.5, 3.0, "epoch"), progress("""{"max_steps":-1,"num_train_epochs":3}""", m))
    }

    @Test fun parsesARun() {
        val r = parseRuns(runs).single()
        assertEquals("failed", r.status)
        assertEquals(1793437923123L, r.startedAt)
        assertEquals(1793429525500L, r.endedAt) // +03:00
        assertNull(r.lastHeartbeatAt)
        assertTrue(r.metrics!!.getValue("train/loss").last.isNaN())
        assertEquals(Double.POSITIVE_INFINITY, r.metrics!!.getValue("train/grad_norm").last, 0.0)
        assertEquals(listOf("git.branch" to "main", "git.dirty" to "false"), flat(r.git, "git."))
        assertEquals(listOf("lora.alpha" to "32", "lora.r" to "16", "lr" to "2e-05", "model" to "Qwen/Qwen3-8B"), flat(r.config))
        val load = SysLoad.of(r.metrics)!!
        assertEquals(21.5, load.cpu!!, 0.0)
        assertEquals(listOf(0, 1), load.gpus.map { it.id })
        assertEquals(97.0, load.gpus[1].util!!, 0.0)
    }

    @Test fun mainMetric() {
        fun run(vararg m: Pair<String, Int>) = parseRuns(runs).single().copy(metrics = m.associate { (k, n) -> k to MetricSummary(n, n, n.toDouble(), 0.0, 0.0) })
        // The shortest key with "loss" in it; sys/ keys never count.
        assertEquals("val/loss", run("train/loss" to 5, "val/loss" to 1, "sys/loss" to 9).mainMetric!!.first)
        assertEquals("eval/loss", run("train/loss" to 5, "eval/loss" to 1).mainMetric!!.first)
        // Else the most logged key, the first by name on a tie.
        assertEquals("acc", run("acc" to 7, "lr" to 3, "sys/cpu" to 99).mainMetric!!.first)
        assertEquals("a", run("b" to 3, "a" to 3).mainMetric!!.first)
        assertNull(run("sys/cpu" to 1).mainMetric)
    }

    @Test fun otherShapes() {
        val s = parseSeries("""{"train/loss":{"step":[10,20,30],"min":[2.1,"NaN",1.5],"max":[2.4,"NaN",1.9],"last":[2.2,"NaN",1.6]}}""")
        assertEquals(listOf(10, 20, 30), s.getValue("train/loss").step)
        assertTrue(s.getValue("train/loss").last[1].isNaN())
        val a = parseAlerts("""[{"id":3,"run":"7f3a","run_name":"r","project":"p","ts":"2026-10-31T09:52:05.000000001Z","level":"error","title":"loss is NaN","body":"a\nb","dismissed_at":null}]""").single()
        assertNull(a.dismissedAt)
        assertEquals("error", a.level)
        val p = parseProjects("""[{"name":"qwen-sft","created_at":"2026-10-29T10:00:00Z","runs":4,"running":2,"last_run_at":null}]""").single()
        assertEquals(Project("qwen-sft", 4, 2, null), p)
        val m = parseMachines("""[{"name":"old-titan","seen_at":"2026-10-31T06:00:00Z","silent":true,"stats":{"cpu":5,"ram_gb":6.1,"gpus":[{"i":0,"util":0,"mem_gb":0.3,"temp_c":36,"power_w":17,"procs":[{"pid":1,"name":"python serve.py","user":"lee","mem_gb":0.5}]}]},"runs":[]}]""").single()
        assertTrue(m.silent)
        assertEquals(listOf("GPU 0 · python serve.py · lee · 0.5 GB"), m.procs)
    }

    // GET /api/v1/agents as the Go server sends it, in its order: newest activity first.
    private val agents = """
        [{"id":"claude:a1","tool":"claude","label":"","host":"","cwd":"/data/src/sweep/","project":null,"metric":"","goal":"min","state":"ended",
          "said":"","report":"","next":"","report_run":null,"report_at":null,"runs":1,"running":0,"kept":0,"last_run":null,
          "started_at":"2026-11-04T09:00:00Z","active_at":"2026-11-04T12:00:00.123456789Z"},
         {"id":"codex:b2","tool":"codex","label":"lr sweep","host":"toji","cwd":"/x","project":"qwen-sft","metric":"eval/acc","goal":"max","state":"working",
          "said":"thinking","report":"lr 2e-5 wins","next":"try 1e-5","report_run":"r9","report_at":"2026-11-04T10:00:00Z","runs":3,"running":1,"kept":1,
          "last_run":{"id":"7f3a","name":"r9","status":"running","ended_at":null},"started_at":"2026-11-04T09:00:00Z","active_at":"2026-11-04T11:00:00Z"},
         {"id":"opencode:c3","tool":"opencode","label":"","host":"","cwd":"","project":null,"metric":"","goal":"min","state":"training",
          "said":"","report":"","next":"","report_run":null,"report_at":null,"runs":0,"running":0,"kept":0,"last_run":null,
          "started_at":"2026-11-04T09:00:00Z","active_at":"2026-11-04T11:30:00Z"},
         {"id":"gemini:d4","tool":"gemini","label":"","host":"","cwd":"/d","project":null,"metric":"","goal":"min","state":"new-in-a-later-server",
          "said":"","report":"","next":"","report_run":null,"report_at":null,"runs":0,"running":0,"kept":0,"last_run":null,
          "started_at":"2026-11-04T09:00:00Z","active_at":"2026-11-04T10:30:00Z"},
         {"id":"claude:e5","tool":"claude","label":"","host":"","cwd":"/e","project":null,"metric":"","goal":"min","state":"waiting",
          "said":"","report":"","next":"","report_run":null,"report_at":null,"runs":0,"running":0,"kept":0,"last_run":null,
          "started_at":"2026-11-04T09:00:00Z","active_at":"2026-11-04T08:00:00Z"}]
    """.trimIndent()

    @Test fun parsesAgents() {
        val a = parseAgents(agents)
        // Whoever needs a person first; training and working tie, so the newest leads; a state this app does not know goes last.
        assertEquals(listOf("claude:e5", "opencode:c3", "codex:b2", "claude:a1", "gemini:d4"), a.map { it.id })
        assertEquals(listOf("Waiting for you", "Training", "Working", "Ended", "new-in-a-later-server"), a.map { it.word })
        // The title: the label, else the last part of cwd, else the tool.
        assertEquals(listOf("e", "opencode", "lr sweep", "sweep", "d"), a.map { it.title })
        val b = a[2]
        assertEquals("qwen-sft", b.project)
        assertEquals(Agent.LastRun("7f3a", "r9"), b.lastRun)
        assertNull(a[3].project)
        assertNull(a[3].lastRun)
    }

    @Test fun agentScore() {
        val run = parseRuns(runs).single()
        val card = parseAgents(agents).single { it.id == "claude:a1" }
        // The project names the metric; the run has not logged it, so there is no number.
        assertNull(card.copy(metric = "eval/acc").score(run))
        assertEquals("eval/loss" to 1.2, card.copy(metric = "eval/loss").score(run))
        // No metric named: the run's own.
        assertEquals(run.mainMetric, card.score(run))
    }

    // A server before 0.5.0 has no /agents, and a failure here must never reach the screen.
    @Test fun agentsNeverFail() = runBlocking {
        val last = parseAgents(agents)
        assertEquals(emptyList<Agent>(), loadAgents({ throw ApiError(404, "not found") }, last))
        assertEquals(last, loadAgents({ throw ApiError(500, "boom") }, last))
        assertEquals(last, loadAgents({ throw java.io.IOException("offline") }, last))
        assertEquals(last, loadAgents({ "<html>a proxy's page</html>" }, last))
        assertEquals(emptyList<Agent>(), loadAgents({ "[]" }, last))
    }

    @Test fun formats() {
        assertEquals("0.5", fmt(0.5))
        assertEquals("1.235", fmt(1.23456))
        assertEquals("0.0002", fmt(2e-4))
        assertEquals("2e-05", fmt(2e-5))
        assertEquals("1.235e+04", fmt(12345.6))
        assertEquals("3410", fmt(3410.0))
        assertEquals("NaN", fmt(Double.NaN))
        assertEquals("2h 5m", duration(2 * 3600 + 5 * 60 + 9.0))
        assertEquals("52m", duration(52 * 60.0))
        assertEquals("0s", duration(-3.0))
    }

    // The demo tells mock.js's story through the same parsers the screens use.
    @Test fun demoStory() = runBlocking {
        val demo = Demo()
        val all = parseRuns(demo.get("/api/v1/runs?metrics=1&limit=200"))
        val by = all.groupBy { it.status }
        assertEquals(setOf("running", "finished", "failed", "crashed"), by.keys)
        assertEquals(listOf("cifar10-resnet", "pixel-diffusion", "qwen-sft"), parseProjects(demo.get("/api/v1/projects")).map { it.name }.sorted())
        assertEquals(listOf("a100-node", "lab-4090", "macbook-pro", "old-titan"), parseMachines(demo.get("/api/v1/hosts")).map { it.name })
        assertTrue(parseMachines(demo.get("/api/v1/hosts")).single { it.name == "old-titan" }.silent)
        val failed = by.getValue("failed").single()
        assertEquals("resnet18-lr0.4", failed.name)
        assertTrue(failed.metrics!!.getValue("train/loss").last.isNaN())
        assertTrue(parseAlerts(demo.get("/api/v1/alerts")).any { it.title == "loss is NaN" && it.run == failed.id })
        val live = by.getValue("running").first { it.name == "dit-s-pixel32" }
        val series = parseSeries(demo.get("/api/v1/runs/${live.id}/metrics?points=300"))
        assertTrue(series.getValue("train/loss").step.size in 2..300)
        assertTrue(parseLogs(demo.get("/api/v1/runs/${live.id}/logs?tail&limit=50")).isNotEmpty())
        assertTrue(parseMedia(demo.get("/api/v1/runs/${live.id}/media")).any { it.kind == "image" })
        val cards = parseAgents(demo.get("/api/v1/agents?hours=24"))
        assertEquals(listOf("waiting", "done", "training"), cards.map { it.state })
        assertTrue(cards.all { c -> all.any { it.id == c.lastRun?.id && it.project == c.project && it.host == c.host } })
        val training = cards.last()
        assertEquals("pixel32-dit", training.title)
        assertEquals("eval/fid", training.score(live)!!.first)
        // A card goes with its run, as the alerts do.
        demo.send("/api/v1/runs/${live.id}", "DELETE")
        assertEquals(listOf("waiting", "done"), parseAgents(demo.get("/api/v1/agents")).map { it.state })
    }
}
