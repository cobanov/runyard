# Eğitim takip sistemi: plan

> Ad: **runyard** (klasör adı hâlâ `training-tracker`). Repo: https://github.com/cobanov/runyard (bkz. §15).
> Plan tarihi: 2026-10-31. Bu dosya geliştirmeye başlayacak oturumun tek giriş noktasıdır.

## 1. Hedef

Kendi sunucunda çalışan, açık kaynak, hafif bir model eğitimi takip sistemi. TeslaMate gibi `docker compose up`
ile kurulur, her şeyi PostgreSQL'e yazar. Eğitimin durumunu, metriklerini, örnek çıktılarını ve log'larını
gösterir, telefona bildirim atar, ajanlar MCP ve CLI ile okur. W&B ve MLflow'dan çok daha az özellik, daha sade
ve daha kullanışlı bir arayüz.

İlk kullanıcıları:
- `~/Developer/gba-diffusion`: GBA'da çalışacak küçük diffusion modeli, eğitimleri toji'de.
- Yakında başlayacak diffusion (LoRA) ve LLM (ince ayar, ön eğitim) eğitimleri.

## 2. Neden kendi projemiz

Araştırma (2026-10-31):
- **Trackio** (Hugging Face, MIT, <3.000 satır Python): yerel öncelikli, kendi sunucusunda çalışabiliyor, W&B
  uyumlu API, resmi MCP sunucusu, `trackio.alert()` + Slack/Discord webhook, Gradio dashboard. Bize en yakın proje.
  Eksikleri: telefona doğrudan push, mobil öncelikli arayüz, çöken/sessiz kalan eğitimi kendiliğinden fark etme,
  terminal çıktısı ve GPU'yu tek ekranda gösterme. Kod ve yöntem alınabilir (MIT: alınan dosyalarda telif notu kalır).
- **W&B**: resmi MCP sunucusu var ama bulut, sunucu tarafı açık kaynak değil.
- **labml**: açık kaynak, mobil öncelikli web uygulaması, ama MCP/ajan desteği yok.
- **ml-lab-mcp, TrainWatcher**: yalnız bildirim tarafı.

Hiçbiri "hafif + kendi sunucunda + ajan dostu + push bildirim + geniş kütüphane desteği" birleşimini sunmuyor.

## 3. İlkeler

1. **Hafif:** tek Go binary + PostgreSQL. Redis, kuyruk, ayrı worker yok.
2. **Kendi API'miz:** hiçbir aracın iç yapısını taklit etmek zorunda değil. Uyumluluk ayrı köprülerde yaşar,
   çekirdeği kısıtlamaz. (W&B uyumluluğu hedef değil; köprü olarak var.)
3. **Geniş entegrasyon:** popüler eğitim kütüphaneleri birinci sınıf destek alır (bkz. §6).
4. **Veri sende kalır:** bulut yok, telemetri yok.
5. **Ajan dostu:** her şey CLI'dan `--json` ile ve MCP'den okunabilir.

## 4. Teknoloji yığını

| Katman | Seçim | Not |
|---|---|---|
| Sunucu | Go (`net/http`, `pgx`) | Web arayüzü `go:embed` ile binary'de. Kullanıcının diğer sunucu projeleri (terminal-army, tshare) de Go |
| Veritabanı | PostgreSQL 17 | Migration'lar sunucu açılışında uygulanır |
| Medya | Disk klasörü (volume), ileride S3/R2 | Postgres'te yalnız kayıt (yol, boyut, adım) |
| Canlı akış | SSE | Cloudflare Tunnel ve `tailscale serve` arkasında sorunsuz |
| Web | Svelte + Vite, grafikler **uPlot** | Mobil öncelikli, PWA |
| Python kütüphanesi | Yalnız standart kütüphane; `numpy`, `pillow`, `pynvml`, `psutil` isteğe bağlı (extras) | Yerel kuyruk + arka plan gönderici |
| MCP | Resmi Go SDK (`modelcontextprotocol/go-sdk`), servisin içinde `/mcp` | Lisans doğrulandı (2026-10-31, LICENSE dosyası): MIT'ten Apache-2.0'a geçişte; yeni kod Apache-2.0, izin vermemiş eski katkılar MIT, dokümanlar CC-BY-4.0. GitHub bu yüzden NOASSERTION gösteriyor. İkisi de bizim lisansla uyumlu |
| API | REST `/api/v1`, OpenAPI şeması sözleşme | Başka dillerde istemci kolay |
| Lisans | Apache-2.0 | Karar verildi (§15) |

Değerlendirilip seçilmeyenler: Python/FastAPI sunucu (daha ağır kurulum), TypeScript/Node (tek avantajı aynı dil),
Elixir (TeslaMate'in seçimi, katkıcı bulmak zor), TimescaleDB (ihtiyaç olursa sonra, şema buna uygun).

## 5. Mimari

```
eğitim betiği ──(Python kütüphanesi: yerel kuyruk → toplu HTTP)──┐
hazır araçlar ──(W&B köprüsü / MLflow ucu / TensorBoard izleyici)─┤
                                                                   ▼
                                   Go sunucusu  ── PostgreSQL (metrik, log, kayıtlar)
                                   │  │  │  └──── medya klasörü (resim, video)
                                   │  │  └─ SSE → web arayüzü (Svelte, PWA)
                                   │  └──── MCP /mcp ← ajanlar (Claude Code vb.)
                                   └─────── bildirimler → ntfy, webhook, Slack/Discord, Telegram, web push
```

### Veri modeli (taslak)

- `projects(id, name UNIQUE, created_at)`
- `runs(id uuid, project_id, name, status, config jsonb, summary jsonb, tags text[], notes, host, git jsonb,
  started_at, ended_at, last_heartbeat_at)`; `status`: `running | finished | failed | crashed`
- `metric_keys(id, project_id, name)`, `UNIQUE(project_id, name)`
- `metrics(run_id, key_id, step bigint, ts timestamptz, value double precision)`, PK `(run_id, key_id, step)`
- `logs(run_id, seq bigint, ts, stream smallint, line text)`
- `media(id, run_id, key, step, kind, path, meta jsonb)`; `kind`: `image | video | text`
- `alerts(id, run_id, ts, level, title, body)`; `level`: `info | warn | error`
- `channels(id, kind, config jsonb)`, `rules(id, project_id, event, condition jsonb, channel_id)`
- `api_keys(id, name, hash, scopes, created_at)`

Metrikler toplu `COPY` ile yazılır. Grafikler ham noktayı değil, aralık başına min/max/son değeri ister
(`?points=1000`); böylece çalıştırma başına milyonlarca satır taşınır.

Durum mantığı: `finish()` → `finished`; Python'da yakalanmayan hata veya sıfırdan farklı çıkış → `failed`
(excepthook + atexit); heartbeat N dakikadır gelmiyorsa sunucu → `crashed` (ve bildirim).

## 6. Veriyi içeri almanın beş yolu

| Yol | Ne zaman | Kapsam |
|---|---|---|
| **1. Python kütüphanesi** | Kodu sen yazıyorsan | Metrik, resim, video, metin örnekleri, terminal çıktısı (stdout/stderr tee), heartbeat, GPU/sistem, `alert()` |
| **2. Framework adaptörleri** | Kodu değiştirebiliyorsan (tek satır) | HF `TrainerCallback` (transformers, TRL, Unsloth), Accelerate `GeneralTracker`, Lightning `Logger`, Keras `Callback`, XGBoost `TrainingCallback`, LightGBM callback, sklearn `autolog`, torchtune `MetricLogger` |
| **3. W&B köprüsü** | Kodunu değiştiremediğin, W&B destekleyen araçlar | Ortam değişkeniyle açılır (`.pth` + import hook ile `import wandb` bize yönlenir). Alt küme: `init`, `log`, `finish`, `config(.update)`, `summary`, `run`, `Image`, `Video`, `Table`, `alert`, `define_metric` (yalnız `step_metric` kullanılır; HF'in `train/global_step`'i adım olur). Desteklenmeyen çağrı uyarı verip atlanır. Yalnız gerçek araçlarla test ederek büyür |
| **4. MLflow uyumlu uç** | MLflow destekleyen araçlar | `/api/2.0/mlflow/...` alt kümesi; `MLFLOW_TRACKING_URI` bize gösterilir. `report_to="mlflow"`, Lightning MLflow logger, `mlflow.autolog` (sklearn, XGBoost, LightGBM, Keras) |
| **5. TensorBoard izleyicisi** | Yalnız TensorBoard yazan araçlar | `<cli> watch ./logs`: tfevents dosyalarını okuyup gönderir (DeepSpeed, kohya, diffusers `--report_to tensorboard`) |

### Kapsama (yakın işler)

| İş | Araç | Yol | Faz |
|---|---|---|---|
| LLM ince ayarı | transformers, TRL, Unsloth | `TrainerCallback`; kod değişmeden W&B köprüsü (`report_to="wandb"`) | v0.1 |
| LLM ön eğitimi / özel döngü | PyTorch, nanoGPT tarzı | Python kütüphanesi veya W&B köprüsü | v0.1 |
| Diffusion LoRA | ai-toolkit (toji, `~/Developer/ai-toolkit`) | W&B köprüsü. `toolkit/logging_aitk.py` doğrudan `wandb.init`, `wandb.log`, `wandb.Image` çağırıyor (doğrulandı) | v0.1 |
| Diffusion | diffusers örnek betikleri, kohya sd-scripts | W&B köprüsü veya TensorBoard izleyici; kendi betiğimizde Accelerate tracker | v0.1 / v0.2 |
| GBA diffusion | `~/Developer/gba-diffusion/training/*.py` | Python kütüphanesi | v0.1 |
| Klasik ML | XGBoost, LightGBM, scikit-learn | Callback'ler veya MLflow ucu | v0.2 |
| Keras/TF, Lightning | | Callback / Logger | v0.2 |

Alana özel ihtiyaçlar:
- **LLM:** değerlendirme adımlarında üretilen metinler tablo olarak (prompt / çıktı / adım), tokens/sn, lr, grad norm.
- **Diffusion:** örnek ızgaraları, adım kaydırıcılı galeri, gürültüden resme geçiş videosu.

## 7. Python kütüphanesi (API taslağı)

```python
import runyard as ry

run = ry.init(project="gba-diffusion", name="pixel32_h512", config={"size": 32, "hidden": 512})
run.log({"loss": 0.031, "lr": 1e-3}, step=1000)          # step serbest; epoch, zaman da eksen olabilir
run.image("samples", grid, step=1000, caption="64 örnek")  # PIL, numpy, yol
run.video("film", frames, fps=30, step=50000)
run.text("generations", [{"prompt": p, "output": o}], step=1000)
run.alert("Kayıp NaN", level="error")
run.finish()

ry.init(...); ry.log(...); ry.finish()                     # tek çalıştırmalı betikler için kısayol
```

- Ayarlar ortam değişkeninden: `RUNYARD_SERVER`, `RUNYARD_API_KEY`, `RUNYARD_PROJECT`.
- Sunucu yoksa veya düşerse yerel kuyrukta (`~/.cache/runyard/`) bekler, gelince gönderir; eğitimi asla durdurmaz.
- Arka plan iş parçacıkları: gönderici, heartbeat (30 sn), sistem/GPU örnekleyici (isteğe bağlı), stdout/stderr tee.
- Aynı süreçte birden fazla çalıştırma desteklenir (açık `run` nesnesi).

## 8. REST API (taslak)

Yazma (istemciler):
- `POST /api/v1/runs` → `{id}`
- `POST /api/v1/runs/{id}/metrics` (toplu: `[{step, ts, values: {k: v}}]`)
- `POST /api/v1/runs/{id}/logs`, `/media` (multipart), `/heartbeat`, `/alerts`
- `PATCH /api/v1/runs/{id}` (durum, summary, bitiş)

Okuma (arayüz, CLI, MCP):
- `GET /api/v1/projects`, `/runs?project=&status=`, `/runs/{id}`
- `GET /api/v1/runs/{id}/metrics?keys=&points=1000`, `/logs?after=`, `/media?key=`
- `GET /api/v1/events` (SSE)

Kimlik: API anahtarı (`Authorization: Bearer`). v0.1 tek kullanıcı.

## 9. Web arayüzü

- **Projeler**
- **Çalıştırmalar:** canlı durum (çalışıyor / bitti / başarısız / çöktü), filtre, etiket
- **Çalıştırma sayfası:** grafikler, config, örnek galerisi (adım kaydırıcı), canlı log, GPU/sistem
- **Karşılaştırma:** seçili çalıştırmaların grafikleri üst üste, config farkı yanında
- Mobilde rahat; ana ekrana PWA olarak eklenir. Sade: her ekran tek bir işi yapar.

## 10. Bildirimler

- **Kanallar:** ntfy, genel webhook (JSON), Slack/Discord, Telegram, web push (PWA)
- **Olaylar:** bitti, başarısız, çöktü/sessiz (heartbeat), NaN/inf, metrik eşiği, koddan `alert()`
- **Kurallar** proje başına. Bildirimde çalıştırmanın bağlantısı ve son örnek resim (ntfy attachment) olur.

## 11. Ajanlar

- **MCP** (`/mcp`, streamable HTTP):
  - Okuma (serbest): `list_projects`, `list_runs`, `get_run`, `get_metrics` (özetlenmiş), `tail_logs`,
    `get_samples` (örnek resimleri ajana resim olarak döner), `compare_runs`, `list_alerts`
  - Yazma (token): not ve etiket ekleme
- **CLI** (Python paketiyle gelir): `runs ls`, `runs show`, `logs -f`, `samples`, `watch` (TensorBoard), hepsi `--json`
- **`SKILL.md`** repoda: "eğitimi başlat, izle, örneklere bak, sorun varsa uyar" akışı

## 12. Kurulum

```yaml
# docker-compose.yml (taslak)
services:
  app:
    image: ghcr.io/cobanov/runyard:latest
    environment:
      DATABASE_URL: postgres://tt:tt@db/tt
      MEDIA_DIR: /media
    volumes: ["./media:/media"]
    ports: ["8080:8080"]
    depends_on: [db]
  db:
    image: postgres:17
    environment: { POSTGRES_USER: tt, POSTGRES_PASSWORD: tt, POSTGRES_DB: tt }
    volumes: ["./pg:/var/lib/postgresql/data"]
```

Yedek: `pg_dump` + medya klasörü. Erişim: `tailscale serve` veya Cloudflare Tunnel.

## 13. Fazlar

**v0.1: kendi işlerimizde kullanılabilir**
1. Repo iskeleti: `server/` (Go), `sdk/python/`, `web/` (Svelte)
2. Şema + migration'lar, yazma uçları, testler
3. Python kütüphanesi: init/log/image/video/text/alert, kuyruk, heartbeat, stdout tee, GPU
4. Okuma uçları + SSE + web arayüzü (liste, çalıştırma sayfası, karşılaştırma)
5. Bildirim: ntfy + genel webhook (bitti, başarısız, çöktü)
6. MCP okuma araçları + CLI
7. Adaptörler: HF `TrainerCallback`, Accelerate tracker, W&B köprüsü
8. docker compose, toji'ye kurulum

**Kabul ölçütü:** şu dört koşu sisteme yazıyor, telefona "bitti" ve "çöktü" bildirimi geliyor, bir ajan sonuçları
MCP'den okuyup örnek resimlere bakabiliyor:
- gba-diffusion kelebek taraması (`training/sweep.sh`)
- bir TRL ince ayarı
- bir ai-toolkit LoRA koşusu (W&B köprüsüyle, kod değişmeden)
- bir diffusers örnek betiği

**v0.2:** Lightning, Keras, XGBoost, LightGBM, sklearn, torchtune adaptörleri; MLflow uyumlu uç; TensorBoard
izleyicisi; NaN/eşik/sessizlik kuralları; PWA push; video oynatıcı.

**v0.3:** checkpoint/artifact kayıtları; çok kullanıcı ve ekipler; S3/R2 medya; upstream PR'lar
(transformers `report_to="runyard"`, Accelerate'e hazır tracker); herkese açık sürüm ve doküman sitesi.

## 14. Riskler

- **W&B köprüsü büyüyebilir:** HF'in WandbCallback'i `define_metric`, `config.update`, `Table` gibi şeyleri de
  çağırıyor. Köprüyü yalnız gerçek araçlarla test ederek ve gerektiği kadar büyüt.
- **MLflow ucunun derinliği belirsiz:** `mlflow.autolog` model dosyalarını da yüklemeye çalışıyor; artifact
  uçlarının ne kadarı gerektiği denemeden bilinmez.
- **LLM ön eğitiminde metrik hacmi:** çalıştırma başına milyonlarca satır. Toplu `COPY` ve özetli sorgu yeterli
  olmalı; olmazsa TimescaleDB.

## 15. Kararlar (2026-10-31)

1. **Proje adı: `runyard`.** Python paketi ve CLI `runyard` (`import runyard as ry`), Go binary'si
   `runyard-server`, imaj `ghcr.io/cobanov/runyard`, ortam değişkenleri `RUNYARD_*`. PyPI'da ve GitHub'da boştu.
2. **Sahiplik:** kişisel açık kaynak, GitHub hesabı `cobanov`, repo https://github.com/cobanov/runyard (baştan açık).
3. **Lisans:** Apache-2.0.

## 16. Ortam notları

Kişisel ortam ayrıntıları (makineler, erişim, bildirim sunucusu) repoda değil, yerel `ops-notes.md`
dosyasında (`.gitignore`'da). Geliştiren oturum önce onu da okur.

## 17. Kaynaklar

- Trackio: https://huggingface.co/docs/trackio/index · MCP: https://huggingface.co/docs/trackio/api_mcp_server ·
  repo: https://github.com/gradio-app/trackio (MIT)
- huggingface-trackio skill'i (uyarılar): https://skills.sh/huggingface/skills/huggingface-trackio
- W&B MCP sunucusu: https://weave-docs.wandb.ai/reference/gen_notebooks/wandb_mcp_server
- labml: https://github.com/labmlai/labml
- ml-lab-mcp: https://glama.ai/mcp/servers/CyrusTao/ml-lab-mcp · TrainWatcher: https://pypi.org/project/trainwatcher/
- MCP Go SDK: https://github.com/modelcontextprotocol/go-sdk
- uPlot: https://github.com/leeoniya/uPlot
