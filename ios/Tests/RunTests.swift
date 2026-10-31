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
