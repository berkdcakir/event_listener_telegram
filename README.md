# Event Listener Backend

Bu proje Arbitrum blockchain'deki belirli kontratları dinler ve Telegram üzerinden bildirim gönderir. Ayrıca Telegram bot komutları ve HTTP API endpoint'leri ile balance sorguları yapabilir.

## Özellikler

### 🔍 Event Dinleme
- Arbitrum blockchain'deki belirli kontratları dinler
- **🔴 ModuleInstalled** eventleri (kırmızı top - en önemli)
- **🟠 Transfer** eventleri (turuncu top - önemli)
- **🔵 Diğer** eventler (mavi top - genel)
- **💰 Özel cüzdan** (0x049A025EA9e0807f2fd38c62923fCe688cBd8460) 250$+ transferleri
- **📢 Gruplandırılmış** bildirimler (aynı anda gelen eventler birleştirilir)

### 🤖 Telegram Bot Komutları
- `/help` - Mevcut komutları listeler

**Hub Balance'ları:**
- `/balanceUsdt` - USDT Hub balance'ını gösterir
- `/balanceEth` - ETH Hub balance'ını gösterir
- `/balanceWbtc` - WBTC Hub balance'ını gösterir

**Ana Kontrat Balance'ları:**
- `/balanceMain` - Ana kontrat ETH balance'ını gösterir
- `/mainUsdt` - Ana kontrat USDT balance'ını gösterir
- `/mainEth` - Ana kontrat ETH balance'ını gösterir
- `/mainWbtc` - Ana kontrat WBTC balance'ını gösterir

### 🌐 HTTP API
- `GET /health` - Sağlık kontrolü
- `GET /balance/:token` - Hub token balance'ı (USDT, ETH, WBTC)
- `GET /balance/main` - Ana kontrat ETH balance'ı
- `GET /balance/main/:token` - Ana kontrat token balance'ı (USDT, ETH, WBTC)

## Kurulum

### Gereksinimler
- Go 1.23+
- Arbitrum RPC endpoint
- Telegram Bot Token

### Ortam Değişkenleri
`.env` dosyasında aşağıdaki değişkenleri tanımlayın:

```env
# Arbitrum RPC
ARBITRUM_RPC=https://arb1.arbitrum.io/rpc

# Telegram Bot
TELEGRAM_BOT_TOKEN=your_bot_token_here
TELEGRAM_CHAT_ID=your_chat_id_here

# API Port (opsiyonel, varsayılan: 8080)
API_PORT=8080

# Backend API URL (Telegram bot için, opsiyonel)
BACKEND_API_URL=http://localhost:8080

# Bootstrap ayarları (opsiyonel)
BOOTSTRAP_BLOCKS=2000
BOOTSTRAP_MAX_WINDOW=500
BOOTSTRAP_NOTIFY=false
```

### Çalıştırma

```bash
# Bağımlılıkları yükle
go mod tidy

# Projeyi derle
go build -o event-listener-backend.exe .

# Çalıştır
./event-listener-backend.exe
```

## API Kullanımı

### Balance Sorguları

```bash
# USDT balance
curl http://localhost:8080/balance/usdt

# ETH balance
curl http://localhost:8080/balance/eth

# WBTC balance
curl http://localhost:8080/balance/wbtc

# Ana kontrat ETH balance
curl http://localhost:8080/balance/main

# Ana kontrat USDT balance
curl http://localhost:8080/balance/main/usdt

# Ana kontrat WBTC balance
curl http://localhost:8080/balance/main/wbtc
```

### Örnek Response

```json
{
  "success": true,
  "token": "USDT",
  "address": "0x3b0794015C9595aE06cf2069C0faC5d9B290f911",
  "balance": "1000.50",
  "symbol": "USDT"
}
```

## Telegram Bot Kullanımı

Bot'u Telegram'da başlattıktan sonra aşağıdaki komutları kullanabilirsiniz:

- `/help` - Tüm komutları listeler
- `/balanceUsdt` - USDT Hub balance'ını gösterir
- `/balanceEth` - ETH Hub balance'ını gösterir
- `/balanceWbtc` - WBTC Hub balance'ını gösterir
- `/balanceMain` - Ana kontrat ETH balance'ını gösterir

**Not:** Bot sadece `/help` komutuna cevap verir. Diğer mesajlara otomatik cevap vermez.

## İzlenen Kontratlar

- **Main App**: `0x33381eC82DD811b1BABa841f1e2410468aeD7047`
- **USDT Hub**: `0x3b0794015C9595aE06cf2069C0faC5d9B290f911`
- **ETH Hub**: `0x845A66F0230970971240d76fdDF7f961e08e3f01`
- **WBTC Hub**: `0xec6595E48933D6f752a6f6421f0a9A019Fb80081`
- **USDC Hub**: `0xEA1523eB5F0ecDdB1875122aC2c9470a978e3010`
- **PAXG Hub**: `0xc5eFb9E4EfD91E68948d5039819494Eea56FFA46`
- **PECTO Hub**: `0xdAE486e75Cdf40bd9B2A0086dCf66e2d6C4e784b`
- **Arb Entrypoint**: `0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789`

## Özel İzleme

- **💰 Özel Cüzdan**: `0x049A025EA9e0807f2fd38c62923fCe688cBd8460`
  - Bu cüzdandan gelen/giden 250$ ve üzeri transferler özel bildirim alır
  - USD değeri otomatik hesaplanır (USDT, WETH, WBTC için)

## Token Kontratları

- **USDT**: `0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9`
- **WETH**: `0x82aF49447D8a07e3bd95BD0d56f35241523fBab1`
- **WBTC**: `0x2f2a2543B76A4166549F7aaB2e75Bef0aefC5B0f`

## Loglar

Uygulama çalıştığında aşağıdaki logları göreceksiniz:

```
🚀 Uygulama başladı!
📡 Event dinleme aktif
🌐 HTTP API aktif
🤖 Telegram bot aktif
```

## Geliştirme

### Proje Yapısı

```
├── main.go                 # Ana uygulama
├── listener/
│   ├── event_watcher.go    # Event dinleme
│   ├── balance.go          # Balance işlemleri
│   ├── wallets.go          # Kontrat adresleri
│   └── config.go           # Konfigürasyon
├── notifier/
│   ├── interface.go        # Notifier interface
│   ├── telegram.go         # Telegram notifier
│   ├── telegram_bot.go     # Telegram bot
│   └── telelog.go          # Telegram log writer
└── internal/app/
    └── api.go              # HTTP API
```

### Yeni Token Ekleme

1. `listener/balance.go` dosyasında `TokenContracts` ve `HubContracts` map'lerine yeni token'ı ekleyin
2. `formatBalance` fonksiyonunda decimal sayısını ayarlayın
3. `internal/app/api.go` dosyasında desteklenen token listesine ekleyin
4. `notifier/telegram_bot.go` dosyasında yeni komut ekleyin
