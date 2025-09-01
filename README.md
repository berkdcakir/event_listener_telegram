# Event Listener Backend

Ethereum (Arbitrum) üzerinde belirli cüzdanlar ve kontratlardan gelen event’leri dinler, anlamlandırır ve Telegram’a bildirim gönderir. ERC-20 transferleri, özel event’ler (ör. InstallModule/DiamondCut) ve native ETH transferleri desteklenir. Önemli olaylar ayrı gruba, normal olaylar ayrı gruba yönlendirilir.
Neler yapar?
Canlı event dinleme: İzlenen adresler için Transfer, InstallModule, DiamondCut ve ABI’den öğrenilen event’ler.
Native ETH tespiti: Log üretmeyen native transferleri blok tarayarak bulur.
USD tahmini: DexScreener/CoinGecko’dan fiyat çekerek USD değeri hesaplar.
Önem derecelendirme: Tutar ve event türüne göre “Önemli/Normal” ayrımı.
Telegram bildirimleri: Gruplandırma, önemli eventlerde alarm akışı ve çift grup desteği.
Profil yönetimi: test ve production cüzdan profilleri.

Kurulum
Gereksinimler
Go 1.21+
Git
(Opsiyonel) Docker/Docker Compose
Telegram Bot Token ve Chat ID’ler
Derleme ve Çalıştırma
# Derleme
go build -o event-listener-backend.exe

# Çalıştırma (Windows örneği)
.\event-listener-backend.exe

Geliştirici Modu
go run . 

Docker ile
docker build -t telegram_bot_listener .
docker run --rm --name telegram_bot_listener --env-file .env telegram_bot_listener

Docker Compose ile
docker-compose up --build

Yapılandırma (Env Değişkenleri)
Aşağıdaki ayarlar .env dosyası ile ya da ortam değişkenleri olarak verilir.
Ağ/RPC
ARBITRUM_RPC: WSS/WS/HTTPS RPC URL’si (zorunlu)
ARBITRUM_HTTP_RPC: Raw HTTP istekler için alternatif URL (opsiyonel)
BACKEND_API_URL: HTTP API base URL (örn: http://3.226.134.195:8080)
Cüzdan Profili
WALLET_PROFILE: test yazılırsa test cüzdanları, aksi halde production cüzdanları yüklenir. Boş → production.
WATCH_EXTRA_ADDRESSES: Virgüllü ek adresler. Örn: 0xabc...,0xdef...
Telegram
TELEGRAM_BOT_TOKEN: Bot token
TELEGRAM_CHAT_ID veya TELEGRAM_CHAT_ID_1: Normal/önemsiz event grubu
TELEGRAM_CHAT_ID_2: Önemli event grubu
Önemli eventler GRUP 2’ye; normal eventler GRUP 1’e gider. Grup yoksa fallback uygulanır.
Fiyatlandırma/Önem
TOKEN_PRICE_CACHE_TTL: Fiyat cache süresi (dk ya da 0.x dakika). Örn: 0.5 (30 sn), 2 (2 dk)
USD_THRESHOLD: Transfer’in “Önemli” sayılacağı USD eşiği. Örn: 50 (default 50)
NATIVE_USD_PRICE: Native coin basit USD tahmini. Örn: 3000
USDC/USDT stable’ları güvenlik için 1.0 USD’ya sabitlenir.
Bootstrap ve Polling
BOOTSTRAP_ENABLE: İlk açılışta geçmiş tarama. false yaparsanız kapatılır.
BOOTSTRAP_BLOCKS: Geçmiş kaç blok taransın (default 2000)
BOOTSTRAP_MAX_WINDOW: Tarama pencere boyutu (default 500)
IMMEDIATE_IMPORTANT: true ise önemli eventler beklemeden gönderilir.
NATIVE_BACKFILL_BLOCKS: Native tarayıcı için geri tarama blok sayısı (opsiyonel)
Debug ve Tanılama
DEBUG_MODE: true olursa detaylı loglar
DIAG_TX_HASH: Belirli bir tx hash için adım adım tanılama log’u üretir
ENABLE_NATIVE_ZERO_TRANSFER_LOGS: true yapmayın; native coin log üretmez (özel senaryolar için)

Örnek .env
# Ağ
ARBITRUM_RPC=wss://arb-mainnet.example/ws
ARBITRUM_HTTP_RPC=https://arb-mainnet.example/http
BACKEND_API_URL=http://3.226.134.195:8080

# Profil
WALLET_PROFILE=prod
WATCH_EXTRA_ADDRESSES=

# Telegram
TELEGRAM_BOT_TOKEN=123456:ABC...
TELEGRAM_CHAT_ID_1=-1001111111111
TELEGRAM_CHAT_ID_2=-1002222222222

# Fiyat/Önem
TOKEN_PRICE_CACHE_TTL=0.5
USD_THRESHOLD=50
NATIVE_USD_PRICE=3000

# Davranışlar
BOOTSTRAP_ENABLE=true
BOOTSTRAP_BLOCKS=2000
BOOTSTRAP_MAX_WINDOW=500
IMMEDIATE_IMPORTANT=false
DEBUG_MODE=false

ÇALIŞTIRMA: go run main.go
# Derleyip çalıştırma
go build -o event-listener-backend.exe
.\event-listener-backend.exe

profil Değiştirme
Production cüzdanları: WALLET_PROFILE boş veya prod/production
Test cüzdanları: WALLET_PROFILE=test

Önem Eşiğini Değiştirme
USD_THRESHOLD=250

Telegram Bildirim Mantığı
Önemli: InstallModule, DiamondCut→InstallModule ve USD tutarı eşik üzeri transferler.
Grup 2’ye gider; ayrıca dört adet kısa “ALARM” mesajı tetiklenir.
Normal: Diğer eventler Grup 1’e.
Gruplar yoksa fallback kuralları ile mesaj kaybolmaz.

Loglar ve Tanılama
DEBUG_MODE=true ile ayrıntılı loglar açılır.
Belirli bir işlem hash’i tanılamak için:
DIAG_TX_HASH=0x....txhash

Uygulama açılışında bu tx için from/to, değer ve uygunsuzluklar loglanır.
Önemli Notlar
Stablecoin’ler (USDC/USDT) güvenlik nedeniyle 1.0 USD’a sabitlenir. Anomali gelirse loglanır.
Bilinmeyen token’lar için fiyat hesaplaması devre dışı; sadece bilinen token listesi üzerinden USD tahmini yapılır.
RPC sağlayıcınız subscribe/push desteklemiyorsa otomatik polling moduna geçilir.
Geliştirme
İzlenen adresler ve kategori etiketleri listener/wallets.go içindeki prodWallets / testWallets dizilerinden gelir.
Token sembolleri ve ondalıkları listener/event_watcher.go içindeki tokenSymbols ve tokenDecimals map’lerinde.
Global event imzaları initGlobalEvents() içinde tanımlı, ABI’lerden de isimler yüklenir.
