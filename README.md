# Event Listener Backend — İkili Mimari (Go Backend + Node viem‑watcher)

Bu proje, Arbitrum ağında belirli adresleri izleyip anlamlandırılmış bildirimler üretmek için iki bileşenli bir mimari kullanır:

- Go tabanlı HTTP API ve Telegram bildirim servisi (Notifier)
- Node.js tabanlı viem-watcher (blok ve log tarayıcı, olay üretici)

Bu ikili yapı sayesinde yüksek istek hacminde esneklik, rate limit toleransı ve kolay gözlemlenebilirlik sağlanır.

## Mimari Genel Bakış

Veri akışı (özet):

1) viem-watcher
- RPC üzerinden blok/log okuma, ERC20 Transfer, önemli kontrat event’leri ve native transfer tespiti
- ANKR API key rotasyonu ve failover ile rate limit’e dayanıklılık
- Tespit edilen olayları HTTP olarak Go Notifier’a POST eder (`/notify`)

2) Go Notifier (Backend)
- `/notify` ile gelen olayları işler, zenginleştirir ve Telegram’a gönderir
- Basit idempotency kontrolü (aynı txHash kısa süreliğine tekrarlanmaz)
- Ek API uçları: health, balance, stats, watch config

Basit şema:

```
[ Arbitrum RPC (ANKR/diğer) ]
              ^
              | (viem)
      +--------------------+
      |  viem-watcher     |
      |  - blok/log tarama|
      |  - ANKR rotasyon  |
      +---------+----------+
                |
                | HTTP POST /notify
                v
      +--------------------+
      |  Go Notifier API   |
      |  - Telegram        |
      |  - Balance/Stats   |
      +--------------------+
```

## Bileşenler

### 1) Node.js viem-watcher (`viem-watcher/src/index.js`)
- viem ile Arbitrum blokları/logları tarar
- İzlenen adresleri `GET /config/watch` üzerinden Backend’den dinamik alabilir
- Önemli event’ler: `Transfer`, `ModuleInstalled`, native ETH transferleri vb.
- Tüm RPC çağrıları `callWithFailover` sarmalayıcısından geçer
  - Rate limit (429/Too Many/limit/quota) veya network hatasında bir sonraki RPC URL’ine (dolayısıyla bir sonraki ANKR key’e) geçer
  - Geçişleri maskeleme ile loglar: `[rpc-failover] switching due to rate-limit/error: ... from i -> j url https://rpc.ankr.com/arbitrum/abcd****`

Kilit env değişkenleri (watcher):
- `RPC_HTTP` veya `ARBITRUM_HTTP_RPC` veya `ANKR_HTTP`: Temel RPC URL (opsiyonel; anahtarlar yoksa kullanışlı)
- `ANKR_API_KEY` veya `ANKR_KEY`: Tekil ANKR anahtarı
- `ANKR_KEYS`, `ANKR_API_KEYS`: Virgülle ayrılmış çoklu key listeleri
- `ANKR_KEY_1.._4`, `ANKR_API_KEY_1.._4`: İndeksli çoklu key değişkenleri
- `NOTIFIER_URL`: Backend `/notify` endpoint’i (varsayılan `http://localhost:8080/notify`)
- `POLL_MS`: Döngü periyodu (ms)
- `CHUNK_BLOCKS`: Her iterasyonda taranacak blok penceresi
- `CONFIRMATIONS`: Onay sayısı (en son bloktan geri çekilme)
- `BACKFILL_BLOCKS`: Başlangıçta geriye dönük taranacak blok sayısı
- `TOKEN_LIST`: Virgüllü ERC20 token adresleri (optimizasyon için)

### 2) Go Notifier Backend
Başlıca endpoint’ler:
- `GET /health`, `GET /healthz`: Sağlık durumu
- `POST /notify`: Watcher’dan gelen olayları Telegram’a iletir
- `GET /config/watch`: Aktif profil için izlenen adresleri döner (watcher bu adresleri kullanabilir)
- `GET /balance/:token`, `GET /balance/main`, `GET /balance/main/:token`: Cüzdan/kontrat bakiyeleri
- `GET /stats/daily`: Basit günlük istatistikler

Telegram gönderimleri:
- MarkdownV2 güvenli biçimleme ve basit rate limit (mesajlar arası bekleme ve Retry-After saygısı)

Kilit env değişkenleri (backend):
- Ağ/RPC: `ARBITRUM_RPC`, `ARBITRUM_HTTP_RPC`, `RPC_HTTP`, `ANKR_HTTP`, `ANKR_API_KEY`
- Profil: `WALLET_PROFILE` (test/prod), `WATCH_EXTRA_ADDRESSES`
- Telegram: `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID_1`, `TELEGRAM_CHAT_ID_2`

Not: Backend tek bir RPC URL’i seçer; Ankr key’i path’e otomatik ekleyebilir. Çoklu key rotasyonu watcher tarafındadır.

## Rate Limit ve ANKR Anahtar Rotasyonu

- viem-watcher, birden fazla ANKR key’i aynı anda konfigüre ederek her biri için birer RPC URL oluşturur
- Tüm zincir çağrıları `callWithFailover` içinde yapılır
  - Hata mesajında `rate / too many / 429 / limit / quota` geçiyorsa veya `FetchError` ise sıradaki URL’e geçiş yapılır
  - Geçiş anı loglanır (key’ler maskeleme ile)
- Böylece yüksek hacimde rate limit’e takılmadan tarama sürdürülebilir

## Çalıştırma

### 1) Sadece Backend (Go)

```bash
# Derleme
go build -o event-listener-backend.exe
# Çalıştırma (Windows)
./event-listener-backend.exe
# Geliştirici modu
go run .
```

Gerekli env’ler için `.env` kullanabilirsiniz.

### 2) Sadece viem-watcher (Node)

```bash
cd viem-watcher
npm ci
# Env’lerinizi (ANKR_KEYS vb.) set edip çalıştırın
node src/index.js
```

Watcher, `NOTIFIER_URL` ile Backend’e bağlanır ve `/notify`’a POST atar.

### 3) Docker / Compose

```bash
# Docker
docker build -t telegram_bot_listener .
docker run --rm --name telegram_bot_listener --env-file .env telegram_bot_listener

# Docker Compose
docker-compose up --build
```

Compose ile hem Backend hem Watcher birlikte ayağa kaldırılabilir (dosyadaki servislere göre).

## Ortam Değişkenleri (Özet)

Backend (Go):
- Ağ/RPC: `ARBITRUM_RPC`, `ARBITRUM_HTTP_RPC`, `RPC_HTTP`, `ANKR_HTTP`, `ANKR_API_KEY`
- Profil: `WALLET_PROFILE`, `WATCH_EXTRA_ADDRESSES`
- Telegram: `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID_1`, `TELEGRAM_CHAT_ID_2`

Watcher (Node):
- RPC: `RPC_HTTP` | `ARBITRUM_HTTP_RPC` | `ANKR_HTTP`
- ANKR Keys: `ANKR_API_KEY` | `ANKR_KEY` | `ANKR_KEYS` | `ANKR_API_KEYS` | `ANKR_KEY_1.._4` | `ANKR_API_KEY_1.._4`
- Diğer: `NOTIFIER_URL`, `POLL_MS`, `CHUNK_BLOCKS`, `CONFIRMATIONS`, `BACKFILL_BLOCKS`, `TOKEN_LIST`

## Gözlem ve Tanılama

- Watcher logları: rate limit/failover durumları `[rpc-failover] switching ...` satırları ile görülebilir
- Backend logları: `/notify` akışı, Telegram gönderimleri, balance/stats hataları
- Health: `GET /health` ve `GET /healthz`

## Güvenlik Notları

- Loglarda RPC key’leri maskeleme yapılır
- Telegram MarkdownV2 kaçışları uygulanır
- Idempotency: Aynı `txHash` kısa süre içinde tekrar edilmez (API tarafında kısa TTL)

## SSS

- S: ANKR key rotasyonu nerede?
  C: Node watcher tarafında. Birden fazla key verdiğinizde otomatik failover yapar.

- S: Backend neden tek RPC kullanıyor?
  C: Backend sorgu hacmi düşük; basitlik için tek uç seçiyor. Yük artarsa aynı failover yaklaşımı Backend’e de eklenebilir.

- S: İzlenecek adresleri nasıl yönetirim?
  C: `WALLET_PROFILE` ve `WATCH_EXTRA_ADDRESSES` ile; watcher `GET /config/watch` ile güncel listenizi alabilir.

---

Eski kısa özet (referans):

- Canlı event dinleme: Transfer, InstallModule vb.
- Native ETH tespiti: Blok tarama ile
- USD tahmini: Piyasa fiyat kaynaklarından
- Önem derecelendirme: Tutar/event türüne göre
- Telegram bildirimleri: Ayrıştırılmış kanal akışları
- Profil yönetimi: test/production
