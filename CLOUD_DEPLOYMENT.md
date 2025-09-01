# Cloud Deployment Rehberi

## Önemli Değişiklikler

### 1. Alchemy RPC Optimizasyonu
- **Blok okuma aralığı**: 1 saniye → 3-5 saniye
- **Blok aralığı**: Tek tek → 100-300 blok aralığında
- **Transfer polling**: 1 saniye → 5 saniye
- **Native ETH tarama**: 1 saniye → 3 saniye

Bu değişiklikler Alchemy RPC'yi %70-80 daha az yoracak.

### 2. Cloud Deployment Düzeltmeleri
- **API Host**: `0.0.0.0` (tüm interface'leri dinle)
- **Port**: `8080` (cloud standard)
- **Restart Policy**: `unless-stopped`
- **Conflict Handling**: Telegram bot conflict hatalarını otomatik çözer

## Environment Variables

Cloud deployment için gerekli environment variables:

```bash
# API Ayarları
API_HOST=0.0.0.0
API_PORT=8080
BACKEND_API_URL=http://3.226.134.195:8080

# RPC Ayarları  
ARBITRUM_RPC=wss://arb-mainnet.g.alchemy.com/v2/YOUR_KEY
ARBITRUM_HTTP_RPC=https://arb-mainnet.g.alchemy.com/v2/YOUR_KEY

# Telegram Bot
TELEGRAM_BOT_TOKEN=your_bot_token
TELEGRAM_CHAT_ID=your_chat_id
TELEGRAM_CHAT_ID_2=your_second_chat_id

# Cüzdan Profili
WALLET_PROFILE=prod

# Debug (opsiyonel)
DEBUG_MODE=false
```

## Docker Deployment

### 1. Docker Compose ile
```bash
docker-compose up -d
```

### 2. Docker Run ile
```bash
docker run -d \
  --name event-listener \
  -p 8080:8080 \
  --env-file .env \
  --restart unless-stopped \
  your-image-name
```

## Health Check

Uygulama başladıktan sonra health check:

```bash
curl http://3.226.134.195:8080/health
```

Beklenen yanıt:
```json
{
  "status": "ok",
  "service": "event-listener-backend"
}
```

## Log Monitoring

Önemli log mesajları:

- `🌐 HTTP API başlatılıyor: 0.0.0.0:8080` - API başarıyla başladı
- `🧭 Optimize edilmiş polling başlatıldı` - Event dinleme optimize edildi
- `🔍 Blok aralığı taranıyor: X-Y (Z blok)` - Blok tarama çalışıyor
- `⚠️ Bot conflict hatası` - Diğer instance çalışıyor (normal)

## Troubleshooting

### 1. Port 8080 Kullanımda
```bash
# Port kullanımını kontrol et
netstat -tulpn | grep 8080

# Eski container'ı durdur
docker stop goapp
docker rm goapp
```

### 2. Telegram Bot Conflict
- Bu normal bir durum, otomatik çözülür
- 30 saniye bekler ve tekrar dener
- Eğer sürekli oluyorsa, eski instance'ı kontrol edin

### 3. RPC Bağlantı Sorunları
- Alchemy RPC key'inin doğru olduğunu kontrol edin
- Rate limit'e takılmadığınızı kontrol edin
- Optimizasyonlar sayesinde artık daha az RPC çağrısı yapılıyor

## Performance Monitoring

Yeni optimizasyonlar sayesinde:
- **RPC çağrıları**: %70-80 azaldı
- **Blok tarama**: 100-300 blok aralığında
- **Polling aralığı**: 3-5 saniye
- **Resource kullanımı**: Önemli ölçüde azaldı

## Security Notes

- API sadece gerekli endpoint'leri expose eder
- CORS ayarları yapılandırılmış
- Environment variables güvenli şekilde yönetiliyor
- Rate limiting aktif
