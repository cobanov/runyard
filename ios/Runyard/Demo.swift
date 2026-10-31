import Foundation
import Synchronization
import UIKit

/// "Try the demo": a made-up runyard server answered inside the app, for App Review and anyone without a server.
/// API.http routes `base` here, so every screen runs unchanged. The story is the web demo's (web/demo/mock.js): runs
/// keep training while the app is open, one goes NaN a few minutes in, and a relaunch starts over.
nonisolated final class DemoServer: URLProtocol {
    static let host = "demo.runyard.invalid"
    static let base = "https://" + host

    override class func canInit(with request: URLRequest) -> Bool { request.url?.host() == host }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() {}

    override func startLoading() {
        let url = request.url!
        let (status, data, type) = Demo.answer(request.httpMethod ?? "GET", url, body: Self.body(request))
        let resp = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": type])!
        client?.urlProtocol(self, didReceive: resp, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }

    /// URLSession hands a protocol the body as a stream, not as httpBody.
    private static func body(_ req: URLRequest) -> Data {
        if let b = req.httpBody { return b }
        guard let s = req.httpBodyStream else { return Data() }
        s.open()
        defer { s.close() }
        var d = Data(), buf = [UInt8](repeating: 0, count: 4096)
        while case let n = s.read(&buf, maxLength: buf.count), n > 0 { d.append(buf, count: n) }
        return d
    }
}

nonisolated enum Demo {
    static let t0 = Date.now.timeIntervalSince1970 // the first request: the story starts when the demo opens
    static let M = 60.0, H = 3600.0, D = 86400.0
    static let removed = Mutex<Set<String>>([]) // run ids deleted with a swipe
    static let dismissed = Mutex<[Int: Double]>([:]) // alert id -> when it was put away

    // Seeded noise, so the history looks the same every time and live steps come from the same functions.
    static func u(_ seed: Int, _ i: Int) -> Double {
        var n = UInt32(truncatingIfNeeded: seed &* 0x27d4eb2f &+ i) ^ 0x9e3779b9
        n = n &* 0x85ebca6b
        n = (n ^ (n >> 13)) &* 0xc2b2ae35
        return Double(n ^ (n >> 16)) / 4294967296
    }
    static func g(_ seed: Int, _ i: Int) -> Double { (u(seed, i) + u(seed + 1, i) + u(seed + 2, i) - 1.5) * 2 }
    static func within(_ r: (Double, Double), _ seed: Int, _ i: Int) -> Double { r.0 + (r.1 - r.0) * u(seed, i) }
    static func cosine(_ s: Double, _ total: Double, _ peak: Double, _ warm: Double) -> Double {
        s < warm ? peak * s / warm : peak * 0.5 * (1 + cos(.pi * (s - warm) / (total - warm)))
    }
    static func iso(_ t: Double) -> String { Date(timeIntervalSince1970: t).ISO8601Format() }
    /// JSON numbers cannot hold NaN or infinity; the server sends them as strings, and so does the demo.
    static func num(_ v: Double) -> Any { v.isFinite ? v : v.isNaN ? "NaN" : v > 0 ? "Infinity" : "-Infinity" }
    static func dur(_ s: Double) -> String { let m = Int((s / 60).rounded()); return m < 60 ? "\(m)m" : "\(m / 60)h \(m % 60)m" }
    static func bar(_ f: Double) -> String { String(repeating: "█", count: Int((f * 20).rounded())).padding(toLength: 20, withPad: " ", startingAt: 0) }
    static func f(_ format: String, _ args: CVarArg...) -> String { String(format: format, arguments: args) }

    // ---- the runs ----

    enum Kind { case sft(plateau: Double, lr: Double, tps: Double, full: Bool), diffusion, cifar(arch: String, lr: Double, epochs: Int, accMax: Double) }
    struct Prof { var util = (0.0, 0.0), mem = 0.0, temp = (33.0, 37.0), power = (18.0, 26.0), cpu = (0.0, 0.0), ram = 0.0 }

    struct Def {
        let seed: Int, project: String, name: String, host: String, gpus: [Int]
        let start: Double, spp: Double, endStep: Int
        var fate = "finished"
        var nanAt: Int? = nil
        let kind: Kind, prof: Prof, proc: String, user: String
        let config: [String: Any]
        var tags: [String] = [], notes = "", branch = "main", dirty = false
        var id: String { "demo-\(seed)" }
        var every: Int { switch kind { case .sft: 10; case .diffusion: 20; case .cifar: 50 } }
        func at(_ s: Int) -> Double { start + Double(s) * spp }
    }

    static let sftProf = Prof(util: (91, 99), mem: 58.3, temp: (61, 69), power: (330, 395), cpu: (14, 26), ram: 46)
    static let ditProf = Prof(util: (95, 100), mem: 14.2, temp: (70, 78), power: (380, 440), cpu: (9, 16), ram: 12)
    static let resProf = Prof(util: (86, 97), mem: 9.7, temp: (64, 72), power: (300, 380), cpu: (20, 32), ram: 8)
    static func qwen(_ o: [String: Any]) -> [String: Any] {
        ["model": "Qwen/Qwen3-8B", "dataset": "ultrachat_200k", "seq_len": 4096, "batch_size": 64, "epochs": 2, "max_steps": 6000].merging(o) { $1 }
    }
    static func res(_ o: [String: Any]) -> [String: Any] {
        ["dataset": "cifar10", "batch_size": 128, "optimizer": "sgd", "momentum": 0.9, "weight_decay": 5e-4].merging(o) { $1 }
    }
    nonisolated(unsafe) static let r16: [String: Any] = ["lora": ["r": 16, "alpha": 32, "dropout": 0.05, "target": "all-linear"], "lr": 2e-4]

    // Runs from the last days; four still training: the r16 baseline, the full fine-tune that goes NaN about 2.5
    // minutes in, the DiT, and resnet50-cosine, which finishes after about a minute.
    nonisolated(unsafe) static let runs: [Def] = [
        Def(seed: 11, project: "qwen-sft", name: "qwen3-8b-lora-r64", host: "a100-node", gpus: [0], start: t0 - 27 * H, spp: 1.9, endStep: 6000,
            kind: .sft(plateau: 1.05, lr: 1e-4, tps: 2950, full: false), prof: Prof(util: (91, 99), mem: 66.9, temp: (61, 69), power: (330, 395), cpu: (14, 26), ram: 46),
            proc: "python train_sft.py", user: "ana", config: qwen(["lora": ["r": 64, "alpha": 128, "dropout": 0.05, "target": "all-linear"], "lr": 1e-4]),
            tags: ["lora", "r64"], notes: "Rank 64: eval/loss 0.06 below r16 for 4x the adapter."),
        Def(seed: 12, project: "qwen-sft", name: "qwen3-8b-lora-r16-nopack", host: "a100-node", gpus: [1], start: t0 - 2 * D - 5 * H, spp: 1.4, endStep: 2010,
            fate: "crashed", kind: .sft(plateau: 1.13, lr: 2e-4, tps: 2400, full: false), prof: sftProf, proc: "python train_sft.py", user: "kai",
            config: qwen(r16.merging(["packing": false]) { $1 }), tags: ["lora", "r16"], notes: "Node rebooted for a driver update mid-run."),
        Def(seed: 13, project: "qwen-sft", name: "qwen3-8b-lora-r16", host: "a100-node", gpus: [0], start: t0 - 52 * M, spp: 1.1, endStep: 6000,
            kind: .sft(plateau: 1.12, lr: 2e-4, tps: 3410, full: false), prof: sftProf, proc: "python train_sft.py", user: "ana",
            config: qwen(r16.merging(["packing": true]) { $1 }), tags: ["lora", "r16", "baseline"], notes: "Baseline LoRA, rank 16 on every linear layer."),
        Def(seed: 14, project: "qwen-sft", name: "qwen3-8b-full-lr2e-5", host: "a100-node", gpus: [1, 2], start: t0 - 38 * M, spp: 2.3, endStep: 6000,
            nanAt: 1060, kind: .sft(plateau: 1.0, lr: 2e-5, tps: 1840, full: true), prof: Prof(util: (94, 100), mem: 79.1, temp: (66, 74), power: (360, 400), cpu: (18, 30), ram: 88),
            proc: "torchrun train_sft.py --deepspeed zero3", user: "kai", config: qwen(["lr": 2e-5, "deepspeed": "zero3", "full_finetune": true]), tags: ["full"]),
        Def(seed: 21, project: "pixel-diffusion", name: "unet-baseline", host: "lab-4090", gpus: [0], start: t0 - 4 * D, spp: 0.21, endStep: 60000,
            kind: .diffusion, prof: Prof(util: (95, 100), mem: 11.8, temp: (70, 78), power: (380, 440), cpu: (9, 16), ram: 12), proc: "python train_unet.py", user: "ana",
            config: ["model": "UNet", "channels": [64, 128, 256], "resolution": 32, "batch_size": 256, "lr": 1e-4, "max_steps": 60000], tags: ["baseline"]),
        Def(seed: 22, project: "pixel-diffusion", name: "dit-s-pixel32-ema0.999", host: "lab-4090", gpus: [0], start: t0 - 30 * H, spp: 0.16, endStep: 80000,
            kind: .diffusion, prof: ditProf, proc: "python train_dit.py", user: "ana",
            config: ["model": "DiT-S/2", "resolution": 32, "patch": 2, "ema": 0.999, "batch_size": 256, "lr": 1e-4, "max_steps": 80000], tags: ["dit"]),
        Def(seed: 23, project: "pixel-diffusion", name: "dit-s-pixel32", host: "lab-4090", gpus: [0], start: t0 - 3 * H - 20 * M, spp: 0.15, endStep: 200_000,
            kind: .diffusion, prof: ditProf, proc: "python train_dit.py", user: "ana",
            config: ["model": "DiT-S/2", "resolution": 32, "patch": 2, "ema": 0.9999, "batch_size": 256, "lr": 1e-4, "max_steps": 200_000],
            tags: ["dit"], notes: "EMA 0.9999; samples every 2,000 steps.", branch: "dit", dirty: true),
        Def(seed: 31, project: "cifar10-resnet", name: "resnet18-lr0.1", host: "lab-4090", gpus: [1], start: t0 - 3 * D, spp: 0.045, endStep: 40000,
            kind: .cifar(arch: "ResNet-18", lr: 0.1, epochs: 100, accMax: 0.953), prof: resProf, proc: "python train.py", user: "kai",
            config: res(["arch": "resnet18", "lr": 0.1, "epochs": 100]), tags: ["sweep-lr"]),
        Def(seed: 32, project: "cifar10-resnet", name: "resnet18-lr0.05", host: "lab-4090", gpus: [1], start: t0 - 3 * D + 40 * M, spp: 0.045, endStep: 40000,
            kind: .cifar(arch: "ResNet-18", lr: 0.05, epochs: 100, accMax: 0.947), prof: resProf, proc: "python train.py", user: "kai",
            config: res(["arch": "resnet18", "lr": 0.05, "epochs": 100]), tags: ["sweep-lr"]),
        Def(seed: 33, project: "cifar10-resnet", name: "resnet18-lr0.4", host: "lab-4090", gpus: [1], start: t0 - 2 * D - 20 * H, spp: 0.045, endStep: 40000,
            nanAt: 2350, kind: .cifar(arch: "ResNet-18", lr: 0.4, epochs: 100, accMax: 0.9), prof: resProf, proc: "python train.py", user: "kai",
            config: res(["arch": "resnet18", "lr": 0.4, "epochs": 100]), tags: ["sweep-lr"]),
        Def(seed: 34, project: "cifar10-resnet", name: "resnet50-cosine", host: "lab-4090", gpus: [1], start: t0 + 75 - 80000 * 0.06, spp: 0.06, endStep: 80000,
            kind: .cifar(arch: "ResNet-50", lr: 0.1, epochs: 200, accMax: 0.962), prof: resProf, proc: "python train.py --arch resnet50", user: "kai",
            config: res(["arch": "resnet50", "lr": 0.1, "epochs": 200, "schedule": "cosine"]), tags: ["resnet50"]),
    ]

    struct State { let step: Int, status: String, ended: Double?, beat: Double }

    static func state(_ r: Def, _ now: Double) -> State {
        let s = max(0, Int((now - r.start) / r.spp)) / r.every * r.every
        if let n = r.nanAt, s >= n { return State(step: n, status: "failed", ended: r.at(n) + 2, beat: r.at(n) + 2) }
        if s >= r.endStep {
            let e = r.at(r.endStep) // a crash is noticed five minutes after the last heartbeat
            return State(step: r.endStep, status: r.fate, ended: r.fate == "crashed" ? e + 5 * M : e, beat: e)
        }
        return State(step: s, status: "running", ended: nil, beat: now)
    }

    /// What a run logs at step s.
    static func log(_ r: Def, _ s: Int) -> [(String, Double)] {
        let x = r.seed, d = Double(s)
        switch r.kind {
        case let .sft(plateau, lr, tps, _):
            if s == r.nanAt { return [("train/loss", .nan), ("train/grad_norm", .infinity)] }
            let creep = r.nanAt.map { pow(max(0, 1 - Double($0 - s) / 150), 2) } ?? 0 // the loss climbs before it breaks
            var m = [("train/loss", plateau + 1.35 * exp(-d / 420) + 0.045 * g(x, s) + creep * 2.5),
                     ("train/learning_rate", cosine(d, 6000, lr, 100)),
                     ("train/grad_norm", 0.32 + 1.1 * exp(-d / 250) + 0.05 * abs(g(x + 5, s)) + creep * 9),
                     ("train/tokens_per_s", (tps + 70 * g(x + 6, s)).rounded())]
            if s % 200 == 0 { m.append(("eval/loss", plateau + 0.07 + 1.15 * exp(-d / 520) + 0.008 * g(x + 7, s) + creep * 3)) }
            return m
        case .diffusion:
            var m = [("train/loss", 0.027 + 0.095 * exp(-d / 9000) + 0.0035 * g(x, s)), ("train/lr", s < 1000 ? 1e-4 * d / 1000 : 1e-4)]
            if s % 2000 == 0 { m.append(("eval/fid", 17.5 + 165 * exp(-d / 21000) + 0.7 * g(x + 3, s))) }
            return m
        case let .cifar(_, lr, epochs, accMax):
            if s == r.nanAt { return [("train/loss", .nan)] }
            let ep = d / 400
            var m = [("train/loss", 0.05 + 2.1 * exp(-ep / (7 * (0.1 / lr).squareRoot())) + 0.06 * abs(g(x, s))),
                     ("train/lr", cosine(d, Double(epochs * 400), lr, 400))]
            if s % 400 == 0 {
                m += [("val/acc", min(0.995, accMax * (1 - exp(-ep / 9)) + 0.004 * g(x + 2, s))),
                      ("val/loss", 0.17 + 1.9 * exp(-ep / 8) + 0.02 * g(x + 3, s)), ("epoch", ep)]
            }
            return m
        }
    }

    /// What the SDK samples every 10 s, indexed by wall time so a run and its machine agree.
    static func sys(_ r: Def, _ t: Double) -> [(String, Double)] {
        let i = Int(t / 10), p = r.prof
        var m = [("sys/cpu", within(p.cpu, r.seed + 10, i)), ("sys/ram_gb", p.ram + 0.4 * g(r.seed + 11, i))]
        for gi in r.gpus {
            let v = gpu(p, r.seed + gi, i)
            m += [("sys/gpu\(gi).util", v.util), ("sys/gpu\(gi).mem_gb", v.mem), ("sys/gpu\(gi).temp_c", v.temp), ("sys/gpu\(gi).power_w", v.power)]
        }
        return m
    }

    static func gpu(_ p: Prof, _ seed: Int, _ i: Int) -> (util: Double, mem: Double, temp: Double, power: Double) {
        (min(100, within(p.util, seed + 20, i).rounded()), p.mem + 0.05 * g(seed + 21, i),
         (p.temp.0 + (p.temp.1 - p.temp.0) * (0.5 + 0.35 * sin(Double(i) / 40) + 0.15 * u(seed + 22, i))).rounded(),
         within(p.power, seed + 23, i).rounded())
    }

    static func history(_ r: Def, _ st: State) -> [String: [(Int, Double)]] {
        var h: [String: [(Int, Double)]] = [:]
        for s in stride(from: r.every, through: st.step, by: r.every) { for (k, v) in log(r, s) { h[k, default: []].append((s, v)) } }
        for t in stride(from: r.start + 10, through: st.beat, by: 10) { for (k, v) in sys(r, t) { h[k, default: []].append((Int(t - r.start), v)) } }
        return h
    }

    static func summary(_ rows: [(Int, Double)]) -> [String: Any] {
        let fin = rows.map(\.1).filter(\.isFinite)
        return ["count": rows.count, "step": rows.last!.0, "last": num(rows.last!.1), "min": num(fin.min() ?? .nan), "max": num(fin.max() ?? .nan)]
    }

    /// A key's history cut into at most n buckets of equal step width, as the server does.
    static func series(_ rows: [(Int, Double)], _ n: Int) -> [String: Any] {
        let width = rows.count <= n ? 1 : (rows.last!.0 - rows[0].0 + n) / n
        var step: [Int] = [], lo: [Double] = [], hi: [Double] = [], last: [Double] = [], b = Int.min
        for (s, v) in rows {
            let k = (s - rows[0].0) / width
            if k != b {
                b = k
                step.append(s); lo.append(v); hi.append(v); last.append(v)
                continue
            }
            let i = step.count - 1
            (step[i], last[i]) = (s, v)
            if v.isFinite {
                lo[i] = lo[i].isFinite ? min(lo[i], v) : v
                hi[i] = hi[i].isFinite ? max(hi[i], v) : v
            }
        }
        return ["step": step, "min": lo.map(num), "max": hi.map(num), "last": last.map(num)]
    }

    static func pub(_ r: Def, _ now: Double, metrics: Bool) -> [String: Any] {
        let st = state(r, now)
        var o: [String: Any] = [
            "id": r.id, "project": r.project, "name": r.name, "status": st.status, "config": r.config, "tags": r.tags, "notes": r.notes,
            "host": r.host, "git": ["branch": r.branch, "commit": (0..<40).map { String(Int(u(r.seed + 7, $0) * 16), radix: 16) }.joined(), "dirty": r.dirty],
            "started_at": iso(r.start), "ended_at": st.ended.map(iso) ?? NSNull(), "last_heartbeat_at": iso(st.beat), "summary": [String: Any](),
        ]
        if metrics {
            let h = history(r, st)
            o["metrics"] = h.mapValues(summary)
            if st.status != "running" { o["summary"] = h.filter { !$0.key.hasPrefix("sys/") }.mapValues { num($0.last!.1) } }
        }
        return o
    }

    static func lines(_ r: Def, _ st: State) -> [(Double, Int, String)] {
        var out: [(Double, Int, String)] = []
        func say(_ t: Double, _ text: String, _ stream: Int = 1) { out.append((t, stream, text)) }
        switch r.kind {
        case let .sft(_, _, _, full):
            say(r.start, "Loading Qwen/Qwen3-8B in bf16 on \(r.gpus.count) GPU\(r.gpus.count > 1 ? "s" : "")")
            say(r.start + 2, full ? "DeepSpeed ZeRO-3: 8,190,735,360 parameters, all trainable" : "trainable params: 43,646,976 || all params: 8,234,382,336 || trainable%: 0.5301")
            for l in ["***** Running training *****", "  Num examples = 207,865", "  Num Epochs = 2", "  Total optimization steps = 6,000"] { say(r.start + 3, l) }
            say(r.start + 4, "UserWarning: torch.utils.checkpoint: the use_reentrant parameter should be passed explicitly.", 2)
        case .diffusion:
            say(r.start, "DiT-S/2 at 32x32, 32.9M parameters, EMA 0.9999, batch 256")
            say(r.start + 1, "dataset: 48,210 sprites from /data/pixel32 (4 classes)")
        case let .cifar(arch, lr, epochs, _):
            say(r.start, "\(arch) on CIFAR-10, \(epochs) epochs, SGD lr \(lr) momentum 0.9, cosine")
        }
        for s in stride(from: r.every, through: st.step, by: r.every) {
            let t = r.at(s), m = Dictionary(log(r, s)) { $1 }, loss = m["train/loss"]!
            switch r.kind {
            case let .sft(_, lr, _, _):
                if s == r.nanAt {
                    say(t, f("{'loss': nan, 'grad_norm': inf, 'learning_rate': %.3e}", cosine(Double(s), 6000, lr, 100)))
                    for l in ["Traceback (most recent call last):", "  File \"/data/src/train_sft.py\", line 212, in <module>", "    trainer.train()",
                              "ValueError: loss is NaN at step \(s); stopping (runyard check: nan train/loss)"] { say(t + 1, l, 2) }
                    continue
                }
                let epoch = Double(s) / 3000
                say(t, f("{'loss': %.4f, 'grad_norm': %.3f, 'learning_rate': %.3e, 'epoch': %.2f}", loss, m["train/grad_norm"]!, m["train/learning_rate"]!, epoch))
                if s % 100 == 0 {
                    say(t, "\(String(100 * s / 6000).leftPad(3))%|\(bar(Double(s) / 6000))| \(s)/6000 [\(dur(Double(s) * r.spp))<\(dur(Double(6000 - s) * r.spp)), \(f("%.2f", r.spp))s/it]")
                }
                if let ev = m["eval/loss"] { say(t, f("{'eval_loss': %.4f, 'eval_runtime': %.1f, 'epoch': %.2f}", ev, 41 + u(r.seed, s) * 3, epoch)) }
                if s % 1000 == 0 { say(t, "Saving model checkpoint to /data/runs/\(r.name)/checkpoint-\(s)") }
            case .diffusion:
                if s % 100 == 0 { say(t, f("step %d | loss %.4f | lr 1.00e-04 | %.1f it/s", s, loss, 1 / r.spp)) }
                if let fid = m["eval/fid"] { say(t, f("saved 4 samples at step %d, FID %.1f", s, fid)) }
                if s % 10000 == 0 { say(t, "checkpoint: /data/runs/\(r.name)/ema-\(s).pt") }
            case let .cifar(_, _, epochs, _):
                if s == r.nanAt {
                    say(t, "Epoch \(s / 400 + 1): loss nan", 2)
                    say(t + 1, "RuntimeError: loss is NaN; stopping (runyard check: nan train/loss)", 2)
                } else if let acc = m["val/acc"] {
                    let ep = s / 400
                    say(t, f("Epoch %d/%d [%@] loss %.3f | val_loss %.3f val_acc %.4f | lr %.4f", ep, epochs, bar(Double(ep) / Double(epochs)), loss, m["val/loss"]!, acc, m["train/lr"]!))
                }
            }
        }
        if st.status == "finished" {
            switch r.kind {
            case .sft: say(st.beat, "Training completed. Saving the final adapter to /data/runs/\(r.name)/final")
            case .cifar: say(st.beat, "saved model.pt")
            case .diffusion: say(st.beat, "done")
            }
        }
        return out
    }

    static func media(_ r: Def, _ st: State) -> [[String: Any]] {
        let scenes = ["a castle at dusk", "a pine forest at night", "a beach at noon", "a mountain lake"]
        switch r.kind {
        case .sft:
            return stride(from: 500, through: st.step, by: 500).map {
                ["id": "\(r.seed)-\($0)", "key": "generations", "step": $0, "kind": "text", "url": "/demo/text/\(min(2, $0 / 1500))", "meta": [:] as [String: Any]]
            }
        case .diffusion:
            return stride(from: 2000, through: st.step, by: 2000).flatMap { s in
                scenes.indices.map { ["id": "\(r.seed)-\(s)-\($0)", "key": "samples", "step": s, "kind": "image", "url": "/demo/sprite/\($0)/\(s)", "meta": ["caption": scenes[$0]]] }
            }
        case .cifar: return []
        }
    }

    static func artifacts(_ r: Def, _ st: State) -> [[String: Any]] {
        let one = { (name: String, kind: String, step: Int, size: Double) -> [String: Any] in
            ["id": "\(r.seed)-\(name)", "name": name, "kind": kind, "step": step, "uri": "\(r.host):/data/runs/\(r.name)/\(name)", "size": Int(size)]
        }
        var out: [[String: Any]]
        switch r.kind {
        case let .sft(_, _, _, full):
            let size = full ? 16.4e9 : 175.2e6
            out = stride(from: 1000, through: st.step, by: 1000).map { one("checkpoint-\($0)", "checkpoint", $0, size) }
            if st.status == "finished" { out.append(one("final", "model", st.step, size)) }
        case .diffusion:
            out = stride(from: 10000, through: st.step, by: 10000).map { one("ema-\($0).pt", "checkpoint", $0, 132.1e6) }
        case let .cifar(arch, _, _, _):
            out = st.status == "finished" ? [one("model.pt", "model", st.step, arch == "ResNet-50" ? 94.4e6 : 44.8e6)] : []
        }
        return out.reversed()
    }

    // ---- alerts: from checks, at fixed moments; some in the past, one a minute in, and one per NaN ----

    static func alerts(_ live: [Def], _ now: Double) -> [[String: Any]] {
        let byName = { (n: String) in runs.first { $0.name == n }! }
        var all: [(id: Int, run: Def, at: Double, level: String, title: String, body: String)] = [
            (1, byName("qwen3-8b-lora-r64"), t0 - 26 * H, "info", "eval/loss below 1.15", "check: eval/loss < 1.15, at step 4,200"),
            (2, byName("dit-s-pixel32"), t0 - 41 * M, "warn", "sys/gpu0.temp_c above 78", "check: GPU 0 at 79 °C for 5 minutes"),
            (3, byName("qwen3-8b-full-lr2e-5"), t0 + 50, "warn", "train/grad_norm above 4", "check: train/grad_norm > 4; the loss is climbing too"),
        ]
        for r in runs { if let n = r.nanAt { all.append((r.seed, r, r.at(n), "error", "loss is NaN", "train/loss NaN at step \(n): stopping.\nLower the learning rate or clip gradients.")) } }
        let ids = Set(live.map(\.id)), gone = dismissed.withLock { $0 }
        return all.filter { $0.at <= now && ids.contains($0.run.id) }.sorted { $0.at > $1.at }.map {
            ["id": $0.id, "run": $0.run.id, "run_name": $0.run.name, "project": $0.run.project, "ts": iso($0.at), "level": $0.level,
             "title": $0.title, "body": $0.body, "dismissed_at": gone[$0.id].map(iso) ?? NSNull()]
        }
    }

    // ---- the machines ----

    struct Machine { let name: String, ram: Double, idle: Prof, gpus: Int; var serving = false; var gone: Double? = nil }
    static let machines = [
        Machine(name: "a100-node", ram: 18, idle: Prof(mem: 0.6, temp: (31, 34), power: (52, 60)), gpus: 4, serving: true),
        Machine(name: "lab-4090", ram: 9, idle: Prof(mem: 0.4), gpus: 2),
        Machine(name: "macbook-pro", ram: 21, idle: Prof(), gpus: 0),
        Machine(name: "old-titan", ram: 6, idle: Prof(mem: 0.3, temp: (35, 38), power: (15, 20)), gpus: 1, gone: t0 - 3 * H - 12 * M),
    ]
    static let serve = Prof(util: (12, 58), mem: 61.8, temp: (44, 55), power: (120, 230))

    static func host(_ m: Machine, _ live: [Def], _ now: Double) -> [String: Any] {
        let t = min(m.gone ?? .infinity, (now / 10).rounded(.down) * 10), i = Int(t / 10), x = m.name.count * 101
        let on = live.filter { r in r.host == m.name && r.start <= t && (state(r, now).ended.map { t <= $0 } ?? true) }
        var cpu = 3 + 4 * u(x, i), ram = m.ram + 0.3 * g(x + 1, i)
        let gpus = (0..<m.gpus).map { gi -> [String: Any] in
            let r = on.first { $0.gpus.contains(gi) }, extra = m.serving && gi == 3
            let v = gpu(r?.prof ?? (extra ? serve : m.idle), (r?.seed ?? x) + gi, i)
            let procs: [[String: Any]] = r.map { [["pid": 10000 + r!.seed * 977 + gi, "name": $0.proc, "user": $0.user, "mem_gb": v.mem - 0.4]] }
                ?? (extra ? [["pid": 48211, "name": "python serve.py --model qwen3-32b", "user": "lee", "mem_gb": v.mem - 0.4]] : [])
            return ["i": gi, "util": v.util, "mem_gb": v.mem, "temp_c": v.temp, "power_w": v.power, "procs": procs]
        }
        for r in on { cpu += within(r.prof.cpu, r.seed + 10, i) / (r.gpus.isEmpty ? 1 : 2); ram += r.prof.ram }
        if m.serving { cpu += 6; ram += 24 }
        return ["name": m.name, "seen_at": iso(t), "silent": now - t > 2 * M, "stats": ["cpu": min(100, cpu), "ram_gb": ram, "gpus": gpus],
                "runs": on.filter { state($0, now).status == "running" }.map { ["id": $0.id, "name": $0.name, "project": $0.project] }]
    }

    // ---- sample images: a 32x32 scene under noise that fades as training goes on, as the web demo draws them ----

    static func scene(_ kind: Int, _ seed: Int, _ x: Int, _ y: Int) -> [Double] {
        let skies: [[[Double]]] = [[[40, 30, 90], [240, 120, 90]], [[10, 14, 40], [40, 60, 110]], [[90, 170, 240], [200, 230, 250]], [[120, 160, 220], [230, 200, 170]]]
        let (top, low) = (skies[kind][0], skies[kind][1]), sun = (8 + seed % 16, 7 + seed % 5), fx = Double(x), fy = Double(y)
        var px = (0..<3).map { top[$0] + (low[$0] - top[$0]) * fy / 32 }
        if (x - sun.0) * (x - sun.0) + (y - sun.1) * (y - sun.1) < 10 { px = kind == 1 ? [230, 230, 210] : [255, 220, 120] }
        if kind == 1 && u(seed, x * 97 + y) > 0.97 && y < 18 { px = [220, 220, 255] }
        let hill = 19 + 3 * sin(fx / 4 + Double(seed)), front = 25 + 2 * sin(fx / 2.5 + Double(seed) * 2)
        if fy > hill { px = kind == 2 ? [230, 210, 150] : [40, 90 + Double(kind) * 10, 60] }
        if fy > front { px = kind == 2 ? [40, 120, 200] : kind == 3 ? [50, 90, 160] : [30, 60, 40] }
        if kind == 0 && (20...27).contains(x) && y >= 11 && fy <= hill && !(y < 14 && x % 2 == 1) { px = [70, 60, 80] }
        if kind == 1 && x % 7 == 3 && fy > hill - 7 && fy <= hill + 2 { px = [20, 50, 30] }
        return px
    }

    static func sprite(_ kind: Int, _ step: Int) -> Data {
        let n = 32, seed = kind * 13 + 5, learned = 1 - exp(-Double(step) / 9000)
        let block = learned < 0.25 ? 8 : learned < 0.5 ? 4 : learned < 0.75 ? 2 : 1, amp = 255 * 0.9 * (1 - learned)
        var px = [UInt8](repeating: 255, count: n * n * 4)
        for y in 0..<n {
            for x in 0..<n {
                let c = scene(kind, seed, x - x % block, y - y % block)
                for i in 0..<3 { px[(y * n + x) * 4 + i] = UInt8(clamping: Int((c[i] + amp * (u(seed * 3 + i + step, x * n + y) - 0.5)).rounded())) }
            }
        }
        let image = CGImage(width: n, height: n, bitsPerComponent: 8, bitsPerPixel: 32, bytesPerRow: n * 4, space: CGColorSpaceCreateDeviceRGB(),
                            bitmapInfo: CGBitmapInfo(rawValue: CGImageAlphaInfo.noneSkipLast.rawValue), provider: CGDataProvider(data: Data(px) as CFData)!,
                            decode: nil, shouldInterpolate: false, intent: .defaultIntent)!
        return UIImage(cgImage: image).pngData()!
    }

    static let prompts = ["Explain why the sky is blue in two sentences.", "Write a haiku about a GPU that never sleeps.", "What is the capital of Australia, and why not Sydney?"]
    static let answers = [
        ["The sky is blue blue because the the sky light is is blue.", "GPU GPU warm / fans fans fans spinning spinning / the the night", "The capital of Australia is Sydney Sydney, the capital."],
        ["Sunlight scatters off air molecules, and blue light scatters most, so the sky looks blue.", "Fans hum through the night / gradients fall like snow / loss creeps toward dawn", "Canberra. It was built as a compromise between Sydney and Melbourne."],
        ["Air molecules scatter short wavelengths far more than long ones (Rayleigh scattering), so blue light reaches your eyes from every direction of the sky.",
         "Fans hum, never still / a thousand gradients fall / dawn finds the loss low", "Canberra. Sydney and Melbourne both wanted it, so in 1908 a new city was chosen between them."],
    ]

    // ---- the API: only what the app calls ----

    static func answer(_ method: String, _ url: URL, body: Data) -> (Int, Data, String) {
        let now = Date.now.timeIntervalSince1970
        let p = url.path().split(separator: "/").map { String($0).removingPercentEncoding ?? String($0) }
        var q: [String: String] = [:]
        for item in URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? [] { q[item.name] = item.value ?? "" }
        let gone = removed.withLock { $0 }
        let live = runs.filter { !gone.contains($0.id) }
        let run = p.count > 3 && p[2] == "runs" ? live.first { $0.id == p[3] } : nil
        let ok = { (v: Any) in (200, try! JSONSerialization.data(withJSONObject: v), "application/json") }
        let limit = Int(q["limit"] ?? "") ?? 100

        switch (method, p.dropFirst(2).first, run) {
        case ("GET", _, _) where p.count == 4 && p[0] == "demo" && p[1] == "sprite":
            return (200, sprite(Int(p[2]) ?? 0, Int(p[3]) ?? 0), "image/png")
        case ("GET", _, _) where p.count == 3 && p[0] == "demo" && p[1] == "text":
            let a = answers[min(2, Int(p[2]) ?? 0)]
            return ok(prompts.indices.map { ["prompt": prompts[$0], "response": a[$0]] })
        case ("GET", "me", _): return ok(["auth": false, "user": ""])
        case ("GET", "projects", _):
            let names = live.reduce(into: [String]()) { if !$0.contains($1.project) { $0.append($1.project) } }
            return ok(names.map { name in
                let rs = live.filter { $0.project == name }
                return ["name": name, "runs": rs.count, "running": rs.filter { state($0, now).status == "running" }.count, "last_run_at": iso(rs.map(\.start).max()!)]
            }.sorted { ($0["last_run_at"] as! String) > ($1["last_run_at"] as! String) })
        case ("GET", "runs", nil) where p.count == 3:
            let rs = live.sorted { $0.start > $1.start }
                .filter { r in q["project"].map { r.project == $0 } ?? true }
                .filter { r in q["status"].map { state(r, now).status == $0 } ?? true }
            return ok(rs.prefix(limit).map { pub($0, now, metrics: q["metrics"] == "1") })
        case ("GET", "runs", let r?):
            let st = state(r, now)
            switch p.count > 4 ? p[4] : "" {
            case "": return ok(pub(r, now, metrics: true))
            case "metrics":
                let want = q["keys"].map { Set($0.split(separator: ",").map(String.init)) }
                let n = min(Int(q["points"] ?? "") ?? 600, 2000)
                return ok(history(r, st).filter { want?.contains($0.key) ?? true }.mapValues { series($0, n) })
            case "logs":
                let all = lines(r, st).enumerated().map { ["seq": $0.offset + 1, "ts": iso($0.element.0), "stream": $0.element.1, "line": $0.element.2] as [String: Any] }
                let after = Int(q["after"] ?? "") ?? 0
                return ok(q["tail"] != nil ? Array(all.suffix(limit)) : Array(all.dropFirst(after).prefix(limit)))
            case "media": return ok(media(r, st))
            case "artifacts": return ok(artifacts(r, st))
            default: break
            }
        case ("GET", "alerts", _):
            return ok(Array(alerts(live, now).filter { a in q["run"].map { a["run"] as! String == $0 } ?? true }.prefix(limit)))
        case ("GET", "hosts", _): return ok(machines.map { host($0, live, now) })
        case ("POST", "alerts", _):
            let ids = ((try? JSONSerialization.jsonObject(with: body)) as? [String: Any])?["ids"] as? [Int] ?? []
            let all = alerts(live, now).filter { $0["dismissed_at"] is NSNull }.map { $0["id"] as! Int }
            dismissed.withLock { for id in ids.isEmpty ? all : ids { $0[id] = now } }
            return ok(["dismissed": (ids.isEmpty ? all : ids).count])
        case ("DELETE", "runs", let r?):
            removed.withLock { _ = $0.insert(r.id) }
            return ok([:])
        case ("DELETE", "projects", _) where p.count == 4:
            removed.withLock { $0.formUnion(live.filter { $0.project == p[3] }.map(\.id)) }
            return ok([:])
        default: break
        }
        return (404, Data(#"{"error":"not found"}"#.utf8), "application/json")
    }
}

private extension String {
    nonisolated func leftPad(_ n: Int) -> String { String(repeating: " ", count: max(0, n - count)) + self }
}
