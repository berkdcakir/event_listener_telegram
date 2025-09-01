#!/bin/bash

echo "🔍 Environment Variables Kontrolü"
echo "=================================="

# Gerekli environment variables
required_vars=(
    "TELEGRAM_BOT_TOKEN"
    "TELEGRAM_CHAT_ID"
    "ARBITRUM_RPC"
)

# Opsiyonel environment variables
optional_vars=(
    "TELEGRAM_CHAT_ID_2"
    "API_HOST"
    "API_PORT"
    "BACKEND_API_URL"
    "WALLET_PROFILE"
    "DEBUG_MODE"
)

echo ""
echo "✅ Gerekli Variables:"
for var in "${required_vars[@]}"; do
    if [ -n "${!var}" ]; then
        echo "  ✅ $var: ${!var:0:10}..."
    else
        echo "  ❌ $var: TANIMLI DEĞİL"
    fi
done

echo ""
echo "ℹ️ Opsiyonel Variables:"
for var in "${optional_vars[@]}"; do
    if [ -n "${!var}" ]; then
        echo "  ✅ $var: ${!var}"
    else
        echo "  ℹ️ $var: Tanımlı değil (varsayılan kullanılacak)"
    fi
done

echo ""
echo "🔧 Önerilen Cloud Deployment Ayarları:"
echo "  API_HOST=0.0.0.0"
echo "  API_PORT=8080"
echo "  BACKEND_API_URL=http://3.226.134.195:8080"
echo "  WALLET_PROFILE=prod"
echo "  DEBUG_MODE=false"

echo ""
echo "📊 RPC Optimizasyonları:"
echo "  ✅ Blok okuma: 3-5 saniye aralık"
echo "  ✅ Blok aralığı: 100-300 blok"
echo "  ✅ Transfer polling: 5 saniye"
echo "  ✅ Native ETH: 3 saniye"

echo ""
echo "🚀 Deployment Komutları:"
echo "  docker-compose up -d"
echo "  curl http://3.226.134.195:8080/health"
