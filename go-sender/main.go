package main

import (
	"log"
	"os"
	"strings"
	"time"

	"event-listener-backend/internal/app"
	"event-listener-backend/listener"
	"event-listener-backend/notifier"

	"github.com/joho/godotenv"
)

func mask(s string) string {
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + strings.Repeat("*", len(s)-6) + s[len(s)-3:]
}

// loadEnvFile BOM karakterini temizleyerek .env dosyasını yükler
func loadEnvFile(filename string) error {
	// Dosyayı oku
	content, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	// UTF-16 BOM karakterini temizle (FE FF)
	if len(content) >= 2 && content[0] == 0xFF && content[1] == 0xFE {
		// UTF-16 LE formatını UTF-8'e çevir
		utf16Content := make([]byte, 0, len(content))
		for i := 2; i < len(content)-1; i += 2 {
			if content[i] != 0x00 { // Null byte'ları atla
				utf16Content = append(utf16Content, content[i])
			}
		}
		content = utf16Content
	}

	// UTF-8 BOM karakterini temizle (EF BB BF)
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		content = content[3:]
	}

	// Temizlenmiş içeriği geçici dosyaya yaz
	tempFile := filename + ".temp"
	if err := os.WriteFile(tempFile, content, 0644); err != nil {
		return err
	}
	defer os.Remove(tempFile)

	// godotenv ile yükle
	return godotenv.Load(tempFile)
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("💥 Panic yakalandi: %v", r)
		}
	}()

	// .env dosyalarını daha agresif şekilde yükle (son yüklenen kazanır)
	// BOM karakterini temizle ve .env dosyasını yükle
	if err := loadEnvFile(".env"); err != nil {
		log.Printf("⚠️ .env yükleme hatası: %v", err)
		// Alternatif yolları dene
		_ = godotenv.Load("notifier/.env")
		_ = godotenv.Load("/app/.env")
		_ = godotenv.Load("/workspace/.env")
	} else {
		log.Println("✅ .env dosyası başarıyla yüklendi")
	}

	// Çalışma dizini ve .env varlık kontrolü
	if wd, err := os.Getwd(); err == nil {
		log.Println("📁 CWD:", wd)
	}
	if _, err := os.Stat(".env"); err == nil {
		log.Println("🟢 .env bulundu (proje kökü)")
	} else {
		log.Printf("🟡 .env bulunamadı: %v", err)
	}

	// Environment variable'ları kontrol et
	log.Println("🔍 Environment variable kontrolü:")

	if v := os.Getenv("TELEGRAM_BOT_TOKEN"); v == "" {
		log.Println("❌ TELEGRAM_BOT_TOKEN görülmedi")
	} else {
		log.Println("✅ TELEGRAM_BOT_TOKEN:", mask(v))
	}

	// TELEGRAM_CHAT_ID_1 öncelikli, yoksa TELEGRAM_CHAT_ID
	primaryChat := os.Getenv("TELEGRAM_CHAT_ID_1")
	if primaryChat == "" {
		primaryChat = os.Getenv("TELEGRAM_CHAT_ID")
	}
	if v := primaryChat; v == "" {
		log.Println("❌ TELEGRAM_CHAT_ID görülmedi")
	} else {
		log.Println("✅ TELEGRAM_CHAT_ID:", mask(v))
	}

	if v := os.Getenv("TELEGRAM_CHAT_ID_2"); v == "" {
		log.Println("❌ TELEGRAM_CHAT_ID_2 görülmedi")
	} else {
		log.Println("✅ TELEGRAM_CHAT_ID_2:", mask(v))
	}

	if v := os.Getenv("ARBITRUM_RPC"); v == "" {
		log.Println("❌ ARBITRUM_RPC görülmedi")
	} else {
		log.Println("✅ ARBITRUM_RPC:", v)
	}

	// Cloud deployment için API ayarları
	if v := os.Getenv("API_HOST"); v == "" {
		log.Println("ℹ️ API_HOST ayarlanmamış (varsayılan: 0.0.0.0)")
	} else {
		log.Println("✅ API_HOST:", v)
	}

	if v := os.Getenv("API_PORT"); v == "" {
		log.Println("ℹ️ API_PORT ayarlanmamış (varsayılan: 8080)")
	} else {
		log.Println("✅ API_PORT:", v)
	}

	// Debug mode kontrolü
	if v := os.Getenv("DEBUG_MODE"); v == "" {
		log.Println("ℹ️ DEBUG_MODE ayarlanmamış (varsayılan: false)")
	} else {
		log.Println("🔧 DEBUG_MODE:", v)
	}

	// Cüzdan profillerini env'e göre yükle ve logla
	listener.LoadWalletsFromEnv()
	listener.LogActiveWallets()

	// HTTP API'yi başlat
	router := app.SetupAPI()
	go func() {
		port := os.Getenv("API_PORT")
		if port == "" {
			port = "8080"
		}

		// Cloud deployment için host binding
		host := os.Getenv("API_HOST")
		if host == "" {
			host = "0.0.0.0" // Cloud'da tüm interface'leri dinle
		}

		addr := host + ":" + port
		log.Printf("🌐 HTTP API başlatılıyor: %s", addr)
		if err := router.Run(addr); err != nil {
			log.Printf("❌ HTTP API hatası: %v", err)
		}
	}()

	// Telegram bot'u başlat ve hem komutları hem bildirimleri yönet
	go func() {
		bot, err := notifier.NewTelegramBot()
		if err != nil {
			log.Printf("❌ Telegram bot başlatılamadı: %v", err)
			return
		}

		// Bot instance'ını global olarak ayarla
		listener.SetBotInstance(bot)

		log.Println("🤖 Telegram bot başlatıldı")

		// Bot mesajlarını dinle
		offset := 0
		consecutiveErrors := 0
		maxConsecutiveErrors := 10

		for {
			updates, err := bot.GetUpdates(offset)
			if err != nil {
				consecutiveErrors++
				errMsg := err.Error()

				// Conflict hatası için özel işlem
				if strings.Contains(errMsg, "409 Conflict") {
					log.Printf("⚠️ Bot conflict hatası - diğer instance çalışıyor olabilir. 30 saniye beklenecek...")
					time.Sleep(30 * time.Second)
					consecutiveErrors = 0 // Reset error count
					continue
				}

				log.Printf("❌ Bot update hatası (%d/%d): %v", consecutiveErrors, maxConsecutiveErrors, err)

				// Çok fazla hata varsa daha uzun bekle
				if consecutiveErrors >= maxConsecutiveErrors {
					log.Printf("⚠️ Çok fazla hata, 60 saniye beklenecek...")
					time.Sleep(60 * time.Second)
					consecutiveErrors = 0
				} else {
					time.Sleep(5 * time.Second)
				}
				continue
			}

			// Başarılı istek - hata sayacını sıfırla
			consecutiveErrors = 0

			for _, update := range updates {
				if update.Message.Text != "" {
					// Komut işle (de-dup için message_id gönder)
					if err := bot.HandleCommand(update.Message.Chat.ID, update.Message.Text, update.Message.MessageID); err != nil {
						log.Printf("❌ Komut işleme hatası: %v", err)
					}
				}
				offset = update.UpdateID + 1
			}

			time.Sleep(2 * time.Second) // Cloud'da biraz daha yavaş
		}
	}()

	// Bildirim sistemini başlat (bot instance'ı ile entegre)
	listener.InitNotifiersWithBot()

	// Event listener devre dışı: Eventler viem-watcher üzerinden /notify ile alınacak
	log.Println("🔔 Event kaynağı: viem-watcher -> /notify")

	log.Println("🚀 Uygulama başladı!")
	log.Println("🌐 HTTP API aktif")
	log.Println("🤖 Telegram bot aktif")

	select {}
}
