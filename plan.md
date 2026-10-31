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

Metrikler toplu yazılır: v0.1'de `COPY` yerine `unnest` dizileriyle tek `INSERT ... ON CONFLICT` (istemci aynı partiyi
yeniden gönderebildiği için idempotent olmalı; `COPY` çakışmayı çözemiyor). Grafikler ham noktayı değil, aralık başına min/max/son değeri ister
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
- v0.1: ntfy ve webhook; kanal ve kurallar `runyard-server channel add|ls|rm|test` ile yönetilir (arayüzde ayar
  ekranı yok). Çöktü: 5 dakika heartbeat gelmezse; heartbeat dönerse koşu yeniden `running` olur.
- v0.2 kontrolleri (`checks` tablosu, `runyard-server check add|ls|rm`): `nan KEY`, `above|below KEY DEĞER`
  (`-after ADIM` ısınmayı atlar), `stall SÜRE`. Tetiklenen kontrol bir `alerts` satırı yazar ve bugünkü `alert` olayıyla
  kanallara gider; her kontrol bir koşuda bir kez (`alerts (run_id, check_id)` unique). Kontrol sunucuda olduğu için
  köprü ve adaptörler de kapsanır. Varsayılan tek kontrol: bütün projelerde `nan *loss*` (fp16'da `grad_norm` inf
  olabildiği için yalnız kayıplar). NaN ve eşik metrik yazımında bakılır; zaman damgası 5 dakikadan eski satırlar
  (`runyard sync`) atlanır. Stall 30 sn'lik döngüde, çökme kontrolünden sonra bakılır: `runs.progress_at`, `sys/` dışı
  metrik veya log satırı gelince güncellenir (heartbeat ve sistem örnekleyici ana döngü takılsa da akar). Koşu
  sayfası son 10 alert'i gösterir. Bilerek yok: göreli kurallar, aynı koşuda yeniden kurulma, arayüzde ayar ekranı,
  koddan kural tanımlama.

## 11. Ajanlar

- **MCP** (`/mcp`, streamable HTTP):
  - Okuma (serbest): `list_projects`, `list_runs`, `get_run`, `get_metrics` (özetlenmiş), `tail_logs`,
    `get_samples` (örnek resimleri ajana resim olarak döner), `compare_runs`, `list_alerts`
  - Yazma (token): not ve etiket ekleme
- **CLI** (Python paketiyle gelir): `runs ls`, `runs show`, `logs -f`, `samples`, `watch` (TensorBoard), hepsi `--json`
- **`SKILL.md`** repoda: "eğitimi başlat, izle, örneklere bak, sorun varsa uyar" akışı

## 12. Kurulum

Repo kökündeki `docker-compose.yml`: `app` (çok aşamalı `Dockerfile`: Node ile web derlenir, Go binary'sine gömülür,
imaj distroless/static ve nonroot) + `postgres:17`. Medya ve veritabanı adlandırılmış volume'larda (`media`, `pg`).
Okuma anahtarsız olduğu için port varsayılan olarak yalnız `127.0.0.1:8080`'e açılır (`RUNYARD_BIND` ile değişir);
dışarıya `tailscale serve` ya da Cloudflare Tunnel ile açılır. `RUNYARD_URL` bildirimlerdeki bağlantılar içindir.

```sh
docker compose up -d --build
docker compose exec app /runyard-server key create <ad>
NTFY_TOKEN=... docker compose exec -e NTFY_TOKEN app /runyard-server channel add ntfy https://ntfy.example/runyard
```

Yedek: `pg_dump` + medya volume'u.

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

**Durum (2026-10-31):** v0.1'in sekiz adımı bitti, kabul ölçütü toji'de geçti. Kelebek taramasının 10 koşusu
(kayıp, lr, 10 bin adımda bir EMA örnek ızgarası), TRL SFT (`RunyardCallback` ve `report_to="wandb"` + köprü),
Accelerate `RunyardTracker`, diffusers `train_unconditional.py --logger wandb` ve ai-toolkit FLUX LoRA (köprüyle,
ai-toolkit kodu değişmeden) yazdı. ntfy'ye bitti, hata verdi (traceback'li) ve çöktü (`kill -9` sonrası 5 dk)
bildirimleri örnek resimle geldi. MCP'den `compare_runs` + `get_samples` ile resimlere bakılıp `add_note` ile
yargı yazıldı. Arayüz 390 px genişlikte denendi.

**v0.2:** Lightning, Keras, XGBoost, LightGBM, sklearn, torchtune adaptörleri; MLflow uyumlu uç; TensorBoard
izleyicisi; NaN/eşik/sessizlik kuralları; PWA push; video oynatıcı.

**Durum (2026-10-31): bitti.** Kurallar (§10); Lightning, Keras, XGBoost, LightGBM, sklearn autolog ve
torchtune adaptörleri, her biri gerçek kütüphaneyle küçük bir eğitimle test edildi; `runyard watch` (TensorBoard
olay dosyaları: skaler, resim, metin, hparams; yeniden başlayınca aynı koşuya devam eder); video H.264 mp4
(imageio-ffmpeg yoksa GIF); MLflow uyumlu uç (`/api/2.0/mlflow`, mlflow 3.17 istemcisiyle denendi: start_run/log_*,
sklearn autolog + `models:/` ile geri yüklenen model, Lightning `MLFlowLogger`); web push (VAPID, RFC 8291
şifreleme standart kütüphaneyle, RFC'nin örneğiyle bayt bayt doğrulandı; arayüzde zil düğmesi).

**v0.3:** checkpoint/artifact kayıtları; çok kullanıcı ve ekipler; S3/R2 medya; upstream PR'lar
(transformers `report_to="runyard"`, Accelerate'e hazır tracker); herkese açık sürüm ve doküman sitesi.

**Durum (2026-10-31):** artifact kayıtları (SDK `run.artifact`, HF checkpoint'leri, CLI `runyard artifacts`,
arayüzde sekme, MCP `list_artifacts`); S3/R2 medya (SigV4, SeaweedFS'e karşı test); `RUNYARD_AUTH` ile kullanıcılar
ve ekipler (giriş, ekip başına proje görünürlüğü REST/SSE/MCP/MLflow/bildirimlerde; `user|team|project` komutları);
Slack/Discord/Telegram kanalları; doküman sitesi (`docs/`, MkDocs, GitHub Pages iş akışı) bitti. Kalan, elle
tetiklenecek olanlar: sürüm etiketi, PyPI, Pages ayarı ve upstream PR'lar. Sürüm altyapısı hazır, henüz tetiklenmedi. `.github/workflows/ci.yml` her push'ta
`make test`'in yaptığını yapar; `release.yml` bir `v*` etiketiyle ghcr'a amd64+arm64 imaj, PyPI'a (trusted
publishing) paket ve GitHub release çıkarır, sürüm etiketten gelir. Tek seferlik ayarlar ve adımlar
`RELEASING.md`'de. Upstream PR taslakları `upstream/`'da (yama + PR metni), yerelde gerçek sunucuya karşı
denendi; runyard PyPI'a çıkmadan gönderilmez.

## 14. Riskler

- **W&B köprüsü büyüyebilir:** HF'in WandbCallback'i `define_metric`, `config.update`, `Table` gibi şeyleri de
  çağırıyor. Köprüyü yalnız gerçek araçlarla test ederek ve gerektiği kadar büyüt.
- **MLflow ucunun derinliği belirsiz:** `mlflow.autolog` model dosyalarını da yüklemeye çalışıyor; artifact
  uçlarının ne kadarı gerektiği denemeden bilinmez.
- **LLM ön eğitiminde metrik hacmi:** çalıştırma başına milyonlarca satır. Toplu `INSERT ... unnest` ve özetli sorgu yeterli
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
