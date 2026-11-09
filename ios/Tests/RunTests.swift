import Foundation
import Testing
@testable import Runyard

private func run(config: String, metrics: String) throws -> Run {
    let json = #"{"id":"r","project":"p","name":"n","status":"running","started_at":"2026-10-31T10:00:00.123456+03:00","config":\#(config),"metrics":\#(metrics)}"#
    return try API.decoder.decode(Run.self, from: Data(json.utf8))
}

private func summary(step: Int, last: Double) -> String { #"{"count":1,"step":\#(step),"last":\#(last),"min":0,"max":1}"# }

@Test func progressFromMaxSteps() throws {
    // HF Trainer: max_steps wins over epochs; nested keys count by their last part.
    let r = try run(config: #"{"trainer":{"max_steps":300,"num_train_epochs":3}}"#,
                    metrics: #"{"train/loss":\#(summary(step: 150, last: 0.9)),"sys/cpu":\#(summary(step: 900, last: 40))}"#)
    let p = try #require(r.progress)
    #expect(p.done == 150 && p.total == 300 && p.unit == "step")
}

@Test func progressFromEpochs() throws {
    // max_steps -1 means "train by epochs".
    let r = try run(config: #"{"max_steps":-1,"num_train_epochs":3}"#, metrics: #"{"train/epoch":\#(summary(step: 40, last: 1.5))}"#)
    let p = try #require(r.progress)
    #expect(p.done == 1.5 && p.total == 3 && p.unit == "epoch")
}

@Test func progressFromPlainSteps() throws {
    // ai-toolkit: train.steps, with a sampler's steps beside it.
    let m = #"{"loss/loss":\#(summary(step: 300, last: 0.07))}"#
    let p = try #require(try run(config: #"{"train":{"steps":6000},"sample":{"steps":28,"sample_steps":28}}"#, metrics: m).progress)
    #expect(p.done == 300 && p.total == 6000 && p.unit == "step")
    #expect(try run(config: #"{"sampler":{"steps":28}}"#, metrics: m).progress == nil) // past it: not the run's total
}

@Test func noProgressWithoutATotal() throws {
    #expect(try run(config: #"{"lr":0.001}"#, metrics: #"{"loss":\#(summary(step: 5, last: 1))}"#).progress == nil)
}

@Test func sysLoadReadsGPUs() throws {
    let r = try run(config: "{}", metrics: #"{"sys/cpu":\#(summary(step: 1, last: 28)),"sys/gpu1.util":\#(summary(step: 1, last: 91)),"sys/gpu0.util":\#(summary(step: 1, last: 86)),"sys/gpu0.mem_gb":\#(summary(step: 1, last: 62.5)),"loss":\#(summary(step: 1, last: 1))}"#)
    let load = try #require(SysLoad(r.metrics))
    #expect(load.cpu == 28)
    #expect(load.gpus.map(\.id) == [0, 1])
    #expect(load.gpus[0].util == 86 && load.gpus[0].details == "62.5 GB")
    #expect(SysLoad(["loss": r.metrics!["loss"]!]) == nil)
}

@Test func demoAnswersMeAndRuns() throws {
    let get = { (path: String) in Demo.answer("GET", URL(string: DemoServer.base + path)!, body: Data()) }
    let me = get("/api/v1/me")
    #expect(me.0 == 200)
    #expect(try API.decoder.decode(Me.self, from: me.1).auth == false)
    let runs = try API.decoder.decode([Run].self, from: get("/api/v1/runs?limit=200&metrics=1").1)
    #expect(runs.count == 12)
    #expect(runs.contains { $0.running && $0.mainMetric != nil && $0.progress != nil })
    #expect(runs.contains { $0.status == "crashed" })
    let agents = try API.decoder.decode([Agent].self, from: get("/api/v1/agents?hours=24").1).sorted(by: Agent.urgentFirst)
    #expect(agents.map(\.state) == ["waiting", "done", "training"])
    #expect(agents.allSatisfy { a in runs.contains { $0.id == a.lastRun?.id } })
    let training = try #require(runs.first { $0.id == agents[2].lastRun?.id })
    #expect(training.running && training.score(agents[2].metric)?.key == "eval/fid")
}

@Test func agentCardsSortAndName() throws {
    let card = { (state: String, minute: Int, label: String, cwd: String) in
        #"{"id":"\#(state)\#(minute)","tool":"codex","label":"\#(label)","host":"","cwd":"\#(cwd)","project":null,"metric":"","state":"\#(state)","said":"","report":"","runs":0,"last_run":null,"active_at":"2026-11-04T10:\#(minute):00.123456789Z"}"#
    }
    let json = [card("ended", 50, "", ""), card("working", 10, "", "/data/src/dit/"), card("training", 20, "sweep", "/data/src/dit"),
                card("someday", 59, "", ""), card("waiting", 11, "", ""), card("error", 40, "", "")]
    let agents = try API.decoder.decode([Agent].self, from: Data("[\(json.joined(separator: ","))]".utf8)).sorted(by: Agent.urgentFirst)
    #expect(agents.map(\.state) == ["waiting", "error", "training", "working", "ended", "someday"])
    #expect(agents.map(\.title) == ["codex", "codex", "sweep", "dit", "codex", "codex"])
    #expect(agents[0].word == "Waiting for you" && agents[5].word == "someday")
}

@Test func sysLoadSkipsNaN() throws {
    // Int(NaN) traps where the bars round a value, so a NaN under sys/ must not reach them.
    let nan = #"{"count":1,"step":1,"last":"NaN","min":0,"max":1}"#
    let r = try run(config: "{}", metrics: #"{"sys/cpu":\#(nan),"sys/gpu0.util":"# + #"{"count":1,"step":1,"last":"Infinity","min":0,"max":1}"# + #","sys/gpu0.temp_c":\#(summary(step: 1, last: 61))}"#)
    let load = try #require(SysLoad(r.metrics))
    #expect(load.cpu == nil && load.gpus.count == 1 && load.gpus[0].util == nil && load.gpus[0].details == "61 °C")
    #expect(SysLoad(["sys/cpu": r.metrics!["sys/cpu"]!]) == nil)
}

private func runs(_ statuses: [String: String]) throws -> [Run] {
    let json = statuses.sorted { $0.key < $1.key }.map { #"{"id":"\#($0.key)","project":"p","name":"n-\#($0.key)","status":"\#($0.value)","started_at":"2026-10-31T10:00:00Z","ended_at":"2026-10-31T10:05:00Z"}"# }
    return try API.decoder.decode([Run].self, from: Data("[\(json.joined(separator: ","))]".utf8))
}

private func alerts(_ ids: [Int]) throws -> [Alert] {
    let json = ids.map { #"{"id":\#($0),"run":"a","run_name":"n-a","project":"p","ts":"2026-10-31T10:00:00Z","level":"error","title":"NaN loss","body":"at step 40\nmore","dismissed_at":null}"# }
    return try API.decoder.decode([Alert].self, from: Data("[\(json.joined(separator: ","))]".utf8))
}

@Test func newsSinceTheLastLook() throws {
    // The first look after turning notifications on tells nothing, whatever is on the server.
    // The runs below all ended at 10:05.
    let at = { (minute: Int) in Date(timeIntervalSince1970: 1_793_440_800 + Double(minute) * 60) } // 2026-10-31 10:00 UTC
    let first = news(since: nil, runs: try runs(["a": "running", "b": "running", "c": "finished", "d": "running"]), alerts: try alerts([3, 2]), now: at(0))
    #expect(first.notes.isEmpty)
    #expect(first.seen == Seen(runs: ["a": "running", "b": "running", "c": "finished", "d": "running"], alert: 3, at: at(0)))

    // a failed and b finished since; c had ended before, d still runs; e started and crashed between the two
    // looks; alert 4 is new.
    let next = news(since: first.seen, runs: try runs(["a": "failed", "b": "finished", "c": "finished", "d": "running", "e": "crashed"]), alerts: try alerts([4, 3, 2]), now: at(10))
    #expect(next.notes.map(\.run) == ["a", "b", "e", "a"])
    #expect(next.notes.map(\.title) == ["n-a failed", "n-b finished", "n-e crashed", "NaN loss"])
    #expect(next.notes[0].body == "p, after 5m" && next.notes[3].body == "p/n-a: at step 40")
    #expect(next.seen.alert == 4 && next.seen.runs["a"] == "failed" && next.seen.at == at(10))

    // Nothing changed: nothing to tell, and an emptied alert list does not bring old alerts back later. f is new
    // here but ended before the last look (synced late from an offline cache): not news.
    let quiet = news(since: next.seen, runs: try runs(["a": "failed", "d": "running", "f": "finished"]), alerts: [], now: at(20))
    #expect(quiet.notes.isEmpty && quiet.seen.alert == 4)

    // A look kept by a version that did not store its time: a run it did not know stays quiet.
    let old = try JSONDecoder().decode(Seen.self, from: Data(#"{"runs":{"a":"running"},"alert":0}"#.utf8))
    #expect(news(since: old, runs: try runs(["a": "finished", "e": "finished"]), alerts: []).notes.map(\.run) == ["a"])
}
