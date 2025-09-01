package listener

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"event-listener-backend/notifier"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Event topic hashes
var (
	// ModuleInstalled(bytes32) event topic hash
	moduleInstalledTopic = common.HexToHash("0x84e86d019bcb2870cd4a319c9e0fa1851216ac61b928f6b09ff7a6f8b2218e12")

	// Transfer(address,address,uint256) event topic hash
	transferTopic = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

	// Özel cüzdan adresi
	specialWallet = common.HexToAddress("0x049A025EA9e0807f2fd38c62923fCe688cBd8460")

	// Native ETH bildirimlerinde de-dup icin
	nativeSeen    = make(map[string]time.Time)
	nativeSeenTTL = 10 * time.Minute
)

// Raw RPC client (HTTP) for robust block fetching across tx types
var rawRPCClient *rpc.Client

// removed: mutex no longer needed without parallel backfill

// staticcheck U1000: Her derleme yolunda kullanılmış sayılması için garanti kullanım
func init() {
	_ = moduleInstalledTopic
	_ = transferTopic
	_ = specialWallet
	_ = resolveEventName
	_ = shortHash
	_ = formatEventMessage
	var _ = transferDetails{}
	_ = parseTransferDetails
	_ = estimateUSDValue
	_ = formatWei
	_ = handleLiveEvent
	_ = bootstrapScanWindowed
	_ = subscribeWithReconnect
	_ = pollLogs
	_ = TestImportanceFiltering
}

// adres->(topic0->event adı)
var addressTopicNames = map[string]map[string]string{}

// global topic0->event adı (adres bağımsız bilinen imzalar)
var globalTopicNames = map[string]string{}

func registerGlobalEvent(signature, name string) {
	h := crypto.Keccak256Hash([]byte(signature))
	globalTopicNames[strings.ToLower(h.Hex())] = name
}

func initGlobalEvents() {
	// ERC20/standard
	registerGlobalEvent("Transfer(address,address,uint256)", "Transfer")
	registerGlobalEvent("Approval(address,address,uint256)", "Approval")
	registerGlobalEvent("OwnershipTransferred(address,address)", "OwnershipTransferred")
	// ERC1155-like
	registerGlobalEvent("TransferSingle(address,address,address,uint256,uint256)", "TransferSingle")
	// Diamond
	registerGlobalEvent("DiamondCut((address,uint8,bytes4[])[],address,bytes)", "DiamondCut")
	// Custom from provided ABIs
	registerGlobalEvent("MsgInspectorSet(address)", "MsgInspectorSet")
	registerGlobalEvent("CollateralWithdrawn(uint40,address,uint96)", "CollateralWithdrawn")
	registerGlobalEvent("ColleteralDeposited(uint40,address,uint96)", "ColleteralDeposited")
	registerGlobalEvent("Test_ColleteralDeposited(uint40,address,uint96)", "Test_ColleteralDeposited")
	registerGlobalEvent("ItemListingCancelled(uint40,address,uint48)", "ItemListingCancelled")
	registerGlobalEvent("ItemPriceUpdated(uint40,address,uint48,uint96,uint96)", "ItemPriceUpdated")
	registerGlobalEvent("ItemSold(uint40,address,address,uint48,uint96,uint96,uint80)", "ItemSold")
	registerGlobalEvent("NewItemListing(uint40,address,uint48,uint256)", "NewItemListing")
	registerGlobalEvent("NewItemListingByAdmin(uint40,address,address,bool,uint48,uint256)", "NewItemListingByAdmin")
	registerGlobalEvent("ReferralUsed(address,address,uint256)", "ReferralUsed")
	registerGlobalEvent("groupAdded(address,uint256)", "groupAdded")
	registerGlobalEvent("groupDeleted(address,uint256)", "groupDeleted")
	registerGlobalEvent("groupDrawed(uint32,uint8,uint40)", "groupDrawed")
	registerGlobalEvent("groupNftMintedEvent(string,uint32,uint40,uint40[])", "groupNftMintedEvent")
	registerGlobalEvent("statusUpdated(address,uint256,uint256)", "statusUpdated")
	registerGlobalEvent("InstallmentImported(uint32,uint8,uint32,uint32)", "InstallmentImported")
	registerGlobalEvent("PxSetGrantAccess(address,address,bytes1[])", "PxSetGrantAccess")
	registerGlobalEvent("PxUpdateGrantAccess(address,address,bytes32)", "PxUpdateGrantAccess")
	registerGlobalEvent("DepositedAndCredited((bytes4,address,uint40))", "DepositedAndCredited")
	registerGlobalEvent("MessageReceived(bytes4,address,uint40,uint256)", "MessageReceived")
	registerGlobalEvent("Receipt(address,bytes32,bytes)", "Receipt")
	registerGlobalEvent("Sent((bytes32,uint64,(uint256,uint256)),(uint256,uint256))", "Sent")
	registerGlobalEvent("TransferERC20(address,address,address,uint256)", "TransferERC20")
	registerGlobalEvent("firstDepositPaid(address,uint32,uint40,address,uint96,address,uint256,address,uint96,bool)", "firstDepositPaid")
	registerGlobalEvent("installmentPaid(address,uint32,uint40,uint8,uint96)", "installmentPaid")
	registerGlobalEvent("withdrawPaid(address,uint256,uint256,address,address,bool)", "withdrawPaid")
	registerGlobalEvent("CollateralLiquidated(uint40,address,uint256,uint256)", "CollateralLiquidated")
	registerGlobalEvent("CollateralLiquidatedForInstallment(uint40,address,uint256,uint256)", "CollateralLiquidatedForInstallment")
	registerGlobalEvent("ReceivableAllocatedAsCollateral(uint40,address,uint256,uint256,uint256,address)", "ReceivableAllocatedAsCollateral")
	registerGlobalEvent("SwapExecutedWithAmount(uint40,address,address,uint256,uint256)", "SwapExecutedWithAmount")
	registerGlobalEvent("SwapExecutedWithPercentage(uint40,address,address,uint256,uint256)", "SwapExecutedWithPercentage")
	registerGlobalEvent("rewardsclaimed(address,uint256)", "rewardsclaimed")
}

func resolveEventName(addr common.Address, topic0 common.Hash) string {
	addrKey := strings.ToLower(addr.Hex())
	if m, ok := addressTopicNames[addrKey]; ok {
		if n, ok2 := m[strings.ToLower(topic0.Hex())]; ok2 {
			return n
		}
	}
	if n, ok := globalTopicNames[strings.ToLower(topic0.Hex())]; ok {
		return n
	}
	// kısa hash fallback
	s := topic0.Hex()
	if len(s) > 10 {
		return "Event " + s[:10]
	}
	return "Event " + s
}

// Global notifier listesi
var notifiers []notifier.Notifier

// Bildirim gruplandırma için
type notificationItem struct {
	title string
	body  string
	time  time.Time
}

var (
	notificationBuffer = make(chan notificationItem, 100)
	notificationTicker *time.Ticker
)

// Dinamik token fiyat önbelleği
type tokenPriceEntry struct {
	price    float64
	cachedAt time.Time
}

var (
	tokenPriceCache = make(map[string]tokenPriceEntry)
	tokenPriceTTL   = 30 * time.Second // Varsayılan cache süresi (30 saniye)
)

// getTokenPriceTTL cache süresini ortam değişkeninden alır
func getTokenPriceTTL() time.Duration {
	if v := strings.TrimSpace(os.Getenv("TOKEN_PRICE_CACHE_TTL")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			// Dakika cinsinden değer girilirse
			if f >= 1.0 {
				return time.Duration(f) * time.Minute
			}
			// Saniye cinsinden değer girilirse (0.5 = 30 saniye)
			return time.Duration(f*60) * time.Second
		}
	}
	return tokenPriceTTL
}

// Kısa hash gösterimi
func shortHash(h common.Hash) string {
	s := h.Hex()
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

// getNativeUSDPrice: env'den ya da varsayılandan native coin USD fiyatını döner
func getNativeUSDPrice() float64 {
	// Öncelik: NATIVE_USD_PRICE
	if v := strings.TrimSpace(os.Getenv("NATIVE_USD_PRICE")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	// Alternatif: IMPORTANT_NATIVE_PRICE (geri uyum)
	if v := strings.TrimSpace(os.Getenv("IMPORTANT_NATIVE_PRICE")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	// Varsayılan tahmin
	return 3000.0
}

// RegisterEventName belirli bir adres + topic0 için ad kaydeder
func RegisterEventName(addr common.Address, topic0 common.Hash, name string) {
	addrKey := strings.ToLower(addr.Hex())
	topicKey := strings.ToLower(topic0.Hex())
	m, ok := addressTopicNames[addrKey]
	if !ok {
		m = map[string]string{}
		addressTopicNames[addrKey] = m
	}
	m[topicKey] = name
}

// InitNotifiers bildirim kanallarını başlatır
func InitNotifiers() {
	// Telegram notifier'ı ekle
	if tg, err := notifier.NewTelegramFromEnv(); err == nil {
		notifiers = append(notifiers, tg)
		log.Println("✅ Telegram notifier başlatıldı")
	} else {
		log.Printf("⚠️ Telegram notifier başlatılamadı: %v", err)
	}

	// Bildirim gruplandırma sistemini başlat
	startNotificationProcessor()
}

// InitNotifiersWithBot mevcut bot instance'ı ile bildirim sistemini başlatır
func InitNotifiersWithBot() {
	// Bildirim gruplandırma sistemini başlat
	startNotificationProcessor()
	log.Println("✅ Bildirim sistemi başlatıldı (bot entegrasyonu ile)")
}

// SendNotificationToAllNotifiers tüm aktif notifier'lara bildirim gönderir
func SendNotificationToAllNotifiers(title, body string) {
	// Markdown formatında kalın başlık
	formattedTitle := "*" + escapeMarkdownV2(title) + "*"
	message := fmt.Sprintf("%s\n\n%s", formattedTitle, body)

	// Mevcut notifier'ları kullan
	for _, n := range notifiers {
		if err := n.Notify(message); err != nil {
			log.Printf("❌ Notifier hatası: %v", err)
		}
	}

	// Güvenilir önem tespiti
	isImportantEvent := determineImportance(title, body)

	// Bot instance'ı üzerinden gönder (eğer mevcutsa)
	if bot := getBotInstance(); bot != nil {
		chatID := getChatID()
		chatID2 := getChatID2()

		if isImportantEvent {
			// ÖNEMLİ EVENTLER (ModuleInstalled veya 250+ USDT Transfer): GRUP 2
			if chatID2 != 0 {
				// Önce ana mesajı gönder
				if err := bot.SendMessage(int(chatID2), message); err != nil {
					log.Printf("❌ Bot bildirim (GRUP 2 - ÖNEMLİ) hatası: %v", err)
				} else if strings.ToLower(os.Getenv("DEBUG_MODE")) == "true" {
					log.Printf("✅ Önemli event GRUP 2'ye gönderildi: %s", title)
				}
				// Ardından 4 adet alarm mesajı gönder (1s arayla)
				if strings.ToLower(os.Getenv("DEBUG_MODE")) == "true" {
					log.Printf("🚨 Alarm akışı başlıyor (GRUP2=%d) - title=%q", chatID2, title)
				}
				sendPreAlarms(bot, int(chatID2), 4)
				time.Sleep(1300 * time.Millisecond)
				if strings.ToLower(os.Getenv("DEBUG_MODE")) == "true" {
					log.Printf("🚨 Alarm akışı tamamlandı (GRUP2=%d) - title=%q", chatID2, title)
				}
			} else if chatID != 0 {
				if strings.ToLower(os.Getenv("DEBUG_MODE")) == "true" {
					log.Printf("↩️ GRUP 2 tanımsız, önemli event GRUP 1'e gönderilecek - title=%q", title)
				}
				if err := bot.SendMessage(int(chatID), message); err != nil {
					log.Printf("❌ Bot bildirim (GRUP 1 fallback - ÖNEMLİ) hatası: %v", err)
				} else if strings.ToLower(os.Getenv("DEBUG_MODE")) == "true" {
					log.Printf("⚠️ Önemli event GRUP 1'e fallback gönderildi (GRUP 2 yok): %s", title)
				}
			} else {
				log.Printf("⚠️ Hiçbir TELEGRAM_CHAT_ID tanımlı değil, önemli bildirim atlandı: %s", title)
			}
		} else {
			// NORMAL/ÖNEMSİZ EVENTLER: GRUP 1
			if chatID != 0 {
				if err := bot.SendMessage(int(chatID), message); err != nil {
					log.Printf("❌ Bot bildirim (GRUP 1 - NORMAL) hatası: %v", err)
				} else if strings.ToLower(os.Getenv("DEBUG_MODE")) == "true" {
					log.Printf("✅ Normal event GRUP 1'e gönderildi: %s", title)
				}
			} else if chatID2 != 0 {
				if err := bot.SendMessage(int(chatID2), message); err != nil {
					log.Printf("❌ Bot bildirim (GRUP 2 fallback - NORMAL) hatası: %v", err)
				} else if strings.ToLower(os.Getenv("DEBUG_MODE")) == "true" {
					log.Printf("⚠️ Normal event GRUP 2'ye fallback gönderildi (GRUP 1 yok): %s", title)
				}
			} else {
				log.Printf("⚠️ Hiçbir TELEGRAM_CHAT_ID tanımlı değil, normal bildirim atlandı: %s", title)
			}
		}
	}
}

// sendPreAlarms: Önemli eventlerden önce dikkat çekici 4 ayrı alarm mesajı gönderir
func sendPreAlarms(bot *notifier.TelegramBot, chatID int, count int) {
	alarmText := "🚨 ALARM"
	for i := 0; i < count; i++ {
		if err := bot.SendMessage(chatID, alarmText); err != nil {
			log.Printf("❌ Alarm mesajı gönderilemedi (index=%d, chat=%d): %v", i, chatID, err)
		} else if strings.ToLower(os.Getenv("DEBUG_MODE")) == "true" {
			log.Printf("✅ Alarm gönderildi (index=%d, chat=%d)", i, chatID)
		}
		// Mesajlar arasında 1000ms gecikme
		time.Sleep(1000 * time.Millisecond)
	}
}

// determineImportance: başlık ve gövdeye göre güvenilir önem tespiti
func determineImportance(title, body string) bool {
	// Debug log (sadece DEBUG_MODE=true ise)
	if strings.ToLower(os.Getenv("DEBUG_MODE")) == "true" {
		log.Printf("🔍 Önem tespiti: '%s'", title)
	}

	// 1) ModuleInstalled her zaman önemli
	{
		lt := strings.ToLower(title)
		if strings.Contains(lt, "moduleinstalled") || strings.Contains(lt, "installmodule") || (strings.Contains(lt, "install") && strings.Contains(lt, "module")) {
			if strings.ToLower(os.Getenv("DEBUG_MODE")) == "true" {
				log.Printf("✅ ModuleInstalled tespit edildi - ÖNEMLİ (Grup 2)")
			}
			return true
		}

		// Başlıkta yoksa gövdede anahtar kelimeleri ara (gruplu bildirimler için)
		lb := strings.ToLower(body)
		if strings.Contains(lb, "moduleinstalled") || strings.Contains(lb, "installmodule") || strings.Contains(lb, "diamondcut→installmodule") || (strings.Contains(lb, "install") && strings.Contains(lb, "module")) {
			if strings.ToLower(os.Getenv("DEBUG_MODE")) == "true" {
				log.Printf("✅ Body içinde InstallModule tespit edildi - ÖNEMLİ (Grup 2)")
			}
			return true
		}
	}

	// 2) DiamondCut'i InstallModule olarak da önemli say
	if strings.Contains(strings.ToLower(title), "diamondcut") {
		if strings.ToLower(os.Getenv("DEBUG_MODE")) == "true" {
			log.Printf("✅ DiamondCut tespit edildi - InstallModule olarak işaretlendi (Grup 2)")
		}
		return true
	}

	// 3) Transfer eventleri: Tek USD eşiği (env ile değiştirilebilir)
	if strings.Contains(title, "Transfer") {
		// USD değerini çıkar
		usd := extractUSDFromBody(body)

		// USD eşiği: varsayılan 50$, env ile değiştirilebilir
		threshold := 50.0
		if v := strings.TrimSpace(os.Getenv("USD_THRESHOLD")); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
				threshold = f
			}
		}

		isImportant := usd >= threshold

		if strings.ToLower(os.Getenv("DEBUG_MODE")) == "true" {
			log.Printf("💰 Transfer USD: $%.2f, Eşik: $%.2f, Önemli: %v", usd, threshold, isImportant)

			if isImportant {
				log.Printf("✅ Transfer önemli tespit edildi - ÖNEMLİ (Grup 2)")
			} else {
				log.Printf("ℹ️ Transfer normal tespit edildi - NORMAL (Grup 1)")
			}
		}

		return isImportant
	}

	// 4) Diğer tüm eventler önemsiz (grup 1'e gidecek)
	if strings.ToLower(os.Getenv("DEBUG_MODE")) == "true" {
		log.Printf("ℹ️ Diğer event tespit edildi - NORMAL (Grup 1)")
	}
	return false
}

// extractUSDFromBody: gövdedeki ~$ miktarını parse eder
func extractUSDFromBody(body string) float64 {
	// Örnek satır: "💵 **USD:** `~$1234.56`"
	// Basit arama: ilk "$" sonrası sayıyı topla
	idx := strings.Index(body, "$")
	if idx == -1 {
		return 0
	}
	// Sonraki karakterlerden rakam ve nokta topla
	i := idx + 1
	j := i
	for j < len(body) {
		c := body[j]
		if (c >= '0' && c <= '9') || c == '.' {
			j++
			continue
		}
		break
	}
	if j <= i {
		return 0
	}
	num := body[i:j]
	if f, err := strconv.ParseFloat(num, 64); err == nil {
		return f
	}
	return 0
}

// Global bot instance referansı
var globalBot *notifier.TelegramBot

// SetBotInstance global bot instance'ını ayarlar
func SetBotInstance(bot *notifier.TelegramBot) {
	globalBot = bot
}

// getBotInstance global bot instance'ını döner
func getBotInstance() *notifier.TelegramBot {
	return globalBot
}

// getChatID chat ID'yi ortam değişkeninden alır
func getChatID() int64 {
	// Önce TELEGRAM_CHAT_ID_1, yoksa TELEGRAM_CHAT_ID
	chatIDStr := os.Getenv("TELEGRAM_CHAT_ID_1")
	if chatIDStr == "" {
		chatIDStr = os.Getenv("TELEGRAM_CHAT_ID")
	}
	if chatIDStr == "" {
		return 0
	}

	// String'i int64'e çevir
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		log.Printf("⚠️ TELEGRAM_CHAT_ID parse hatası: %v", err)
		return 0
	}

	return chatID
}

// getChatID2 ikinci chat ID'yi ortam değişkeninden alır
func getChatID2() int64 {
	chatIDStr := os.Getenv("TELEGRAM_CHAT_ID_2")
	if chatIDStr == "" {
		return 0
	}
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		log.Printf("⚠️ TELEGRAM_CHAT_ID_2 parse hatası: %v", err)
		return 0
	}
	return chatID
}

// escapeMarkdownV2 Telegram MarkdownV2 için özel karakterleri escape eder
func escapeMarkdownV2(text string) string {
	// Telegram MarkdownV2'de escape edilmesi gereken karakterler
	escapeChars := []string{
		"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!",
	}

	result := text
	for _, char := range escapeChars {
		result = strings.ReplaceAll(result, char, "\\"+char)
	}

	return result
}

// TestImportanceFiltering filtreleme mantığını test etmek için yardımcı fonksiyon
func TestImportanceFiltering() {
	// Sadece debug mode'da çalıştır
	if strings.ToLower(os.Getenv("DEBUG_MODE")) != "true" {
		return
	}

	log.Println("🧪 Filtreleme mantığı test ediliyor...")

	// Test case 1: ModuleInstalled (önemli)
	title1 := "🔴 [Test] ModuleInstalled"
	body1 := "Test body"
	isImportant1 := determineImportance(title1, body1)
	log.Printf("ModuleInstalled önemli mi? %v (beklenen: true)", isImportant1)

	// Test case 2: Transfer 100 USDT (önemli - USD eşiğini aşıyor)
	title2 := "🔴 [Test] Transfer"
	body2 := "💵 **USD:** `~$100.00`"
	isImportant2 := determineImportance(title2, body2)
	log.Printf("Transfer 100 USDT önemli mi? %v (beklenen: true)", isImportant2)

	// Test case 3: Transfer 25 USDT (önemsiz - USD eşiğini aşmıyor)
	title3 := "🔵 [Test] Transfer"
	body3 := "💵 **USD:** `~$25.00`"
	isImportant3 := determineImportance(title3, body3)
	log.Printf("Transfer 25 USDT önemli mi? %v (beklenen: false)", isImportant3)

	// Test case 4: Diğer event (önemsiz)
	title4 := "🔵 [Test] Approval"
	body4 := "Test body"
	isImportant4 := determineImportance(title4, body4)
	log.Printf("Approval önemli mi? %v (beklenen: false)", isImportant4)

	log.Println("✅ Filtreleme testi tamamlandı")
}

func formatEventMessage(lg types.Log) (string, string) {
	cat := GetCategoryLabel(lg.Address)
	tx := lg.TxHash.Hex()

	// InstallModule event - Kırmızı top (en önemli)
	if len(lg.Topics) > 0 && lg.Topics[0] == moduleInstalledTopic {
		moduleId := hex.EncodeToString(lg.Data)
		title := "🔴 [" + cat + "] InstallModule"
		body := fmt.Sprintf("📋 **Tx:** `%s`\n🔧 **Modül:** `%s`\n⏰ **Zaman:** `%s`", tx, moduleId, time.Now().Format("02.01.2006 15:04:05"))
		return title, body
	}

	// Transfer event
	if len(lg.Topics) > 0 && lg.Topics[0] == transferTopic {
		d := parseTransferDetails(lg)
		if d != nil {
			// Yalnızca izlenen cüzdanları ilgilendiren transferleri bildir
			fromWatched := WatchMap[strings.ToLower(d.from.Hex())]
			toWatched := WatchMap[strings.ToLower(d.to.Hex())]
			if !fromWatched && !toWatched {
				return "", ""
			}
			var body strings.Builder
			body.WriteString(fmt.Sprintf("📋 **Tx:** `%s`\n", tx))
			body.WriteString(fmt.Sprintf("📤 **From:** `%s`\n", d.from.Hex()))
			body.WriteString(fmt.Sprintf("📥 **To:** `%s`\n", d.to.Hex()))
			// Token sembolü ve doğru ondalıkla değer
			sym := getAssetSymbol(lg.Address)
			dec := getTokenDecimals(lg.Address)

			// Debug: Token adresi ve decimal bilgisi
			log.Printf("🔍 Transfer debug: token=%s, symbol=%s, decimal=%d, value=%s, raw_value=%s",
				lg.Address.Hex(), sym, dec, d.value.String(), d.value.String())

			// USDC adresi kontrolü
			if strings.EqualFold(lg.Address.Hex(), "0xaf88d065e77c8cC2239327C5EDb3A432268e5831") ||
				strings.EqualFold(lg.Address.Hex(), "0xEA1523eB5F0ecDdB1875122aC2c9470a978e3010") {
				log.Printf("🔍 USDC transfer tespit edildi! Adres eşleşiyor.")
			} else if strings.EqualFold(lg.Address.Hex(), "0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9") {
				log.Printf("🔍 USDT transfer tespit edildi! Adres eşleşiyor.")
			} else if strings.EqualFold(lg.Address.Hex(), "0x2f2a2543B76A4166549F7aaB2e75Bef0aefC5B0f") {
				log.Printf("🔍 WBTC transfer tespit edildi! Adres eşleşiyor.")
			} else if strings.EqualFold(lg.Address.Hex(), "0xc5eFb9E4EfD91E68948d5039819494Eea56FFA46") {
				log.Printf("🔍 PAXG transfer tespit edildi! Adres eşleşiyor.")
			} else {
				log.Printf("🔍 Bu bilinen token değil! Gelen adres: %s", lg.Address.Hex())
			}

			if sym != "" {
				body.WriteString(fmt.Sprintf("💰 **Value:** `%s %s`\n", formatTokenAmount(d.value, dec), sym))
			} else {
				body.WriteString(fmt.Sprintf("💰 **Value:** `%s`\n", formatTokenAmount(d.value, dec)))
			}
			if d.usdValue > 0 {
				body.WriteString(fmt.Sprintf("💵 **USD:** `~$%.2f`\n", d.usdValue))
			}
			if d.isSpecialWalletInvolved {
				body.WriteString("🚨 **ÖZEL CÜZDAN İLGİLİ**\n")
			}
			body.WriteString(fmt.Sprintf("⏰ **Zaman:** `%s`", time.Now().Format("02.01.2006 15:04:05")))

			// Önem tespiti (emoji seçimi)
			computedBody := body.String()

			// Başlık için token sembolünü kullan (eğer varsa)
			titleCategory := cat
			if sym != "" {
				titleCategory = sym
			}

			isImportant := determineImportance("["+titleCategory+"] Transfer", computedBody)
			emoji := "🔵"
			if isImportant {
				emoji = "🔴"
			}
			title := emoji + " [" + titleCategory + "] Transfer"
			return title, computedBody
		}
	}

	// Diğer eventler - başlıkta event adı (adres ve topic0 kayıtlarından)
	eventName := resolveEventName(lg.Address, lg.Topics[0])
	// DiamondCut'i InstallModule olarak ele al (önemli kabul edilecek)
	if strings.EqualFold(eventName, "DiamondCut") {
		title := "🔴 [" + cat + "] InstallModule"
		body := fmt.Sprintf("📋 **Tx:** `%s`\n⚙️ **Event:** `DiamondCut→InstallModule`\n⏰ **Zaman:** `%s`", tx, time.Now().Format("02.01.2006 15:04:05"))
		return title, body
	}

	title := "🔵 [" + cat + "] " + eventName // Normal (Grup 1)
	body := fmt.Sprintf("📋 **Tx:** `%s`\n⏰ **Zaman:** `%s`", tx, time.Now().Format("02.01.2006 15:04:05"))
	return title, body
}

// parseTransferEvent transfer event'ini parse eder
type transferDetails struct {
	from                    common.Address
	to                      common.Address
	value                   *big.Int
	usdValue                float64
	isSpecialWalletInvolved bool
}

func parseTransferDetails(lg types.Log) *transferDetails {
	if len(lg.Topics) < 3 {
		return nil
	}

	from := common.BytesToAddress(lg.Topics[1].Bytes())
	to := common.BytesToAddress(lg.Topics[2].Bytes())

	var value *big.Int
	if len(lg.Data) >= 32 {
		value = new(big.Int).SetBytes(lg.Data)
	} else {
		value = big.NewInt(0)
	}

	usd := estimateUSDValue(value, lg.Address)
	return &transferDetails{
		from:                    from,
		to:                      to,
		value:                   value,
		usdValue:                usd,
		isSpecialWalletInvolved: isSpecialTransfer(from, to),
	}
}

// isSpecialTransfer özel cüzdan transferi mi kontrol eder
func isSpecialTransfer(from, to common.Address) bool {
	return from == specialWallet || to == specialWallet
}

// estimateUSDValue yaklaşık USD değerini hesaplar
func estimateUSDValue(value *big.Int, tokenAddr common.Address) float64 {
	// Debug: Gelen token adresi
	log.Printf("🔍 estimateUSDValue: token=%s, value=%s", tokenAddr.Hex(), value.String())

	// Dinamik fiyat çek (cache + DexScreener)
	price := fetchTokenUSDPrice(tokenAddr)
	log.Printf("🔍 Fiyat çekildi: price=%f", price)

	if price <= 0 {
		// Debug: USDC için sabit fiyat kullan (1.0)
		if strings.EqualFold(tokenAddr.Hex(), "0xaf88d065e77c8cC2239327C5EDb3A432268e5831") ||
			strings.EqualFold(tokenAddr.Hex(), "0xEA1523eB5F0ecDdB1875122aC2c9470a978e3010") {
			log.Printf("🔍 USDC tespit edildi, sabit fiyat 1.0 kullanılıyor")
			price = 1.0
		} else {
			// Bilinmeyen token'lar için güvenlik kontrolü
			if _, ok := tokenSymbols[strings.ToLower(tokenAddr.Hex())]; !ok {
				log.Printf("🔒 Bilinmeyen token için fiyat hesaplaması devre dışı: %s", tokenAddr.Hex())
				return 0
			}
			log.Printf("🔍 Fiyat bulunamadı ve USDC değil, 0 döndürülüyor")
			return 0
		}
	}

	// Token'ın gerçek decimal'ını kullan
	decimals := getTokenDecimals(tokenAddr)
	log.Printf("🔍 Decimal: %d", decimals)

	// Güvenlik kontrolü: Çok büyük decimal değerleri
	if decimals > 30 {
		log.Printf("⚠️ Çok büyük decimal değeri: %d, varsayılan 18 kullanılıyor", decimals)
		decimals = 18
	}

	divisor := new(big.Float).SetFloat64(math.Pow10(decimals))

	// Token miktarını doğru decimal ile çevir
	valueFloat := new(big.Float).SetInt(value)
	valueFloat.Quo(valueFloat, divisor)

	log.Printf("🔍 Decimal ile bölünmüş değer: %f", valueFloat)

	// USD değerini hesapla
	priceFloat := new(big.Float).SetFloat64(price)
	result := new(big.Float).Mul(valueFloat, priceFloat)
	usdValue, _ := result.Float64()

	// GÜVENLİK KONTROLLERİ
	// 1. Çok büyük USD değerleri (1 milyon dolar üzeri şüpheli)
	if usdValue > 1000000 {
		log.Printf("⚠️ Çok büyük USD değeri tespit edildi: $%.2f, kontrol ediliyor", usdValue)
		// Fiyatı tekrar kontrol et
		price = fetchTokenUSDPrice(tokenAddr)
		if price > 0 {
			priceFloat = new(big.Float).SetFloat64(price)
			result = new(big.Float).Mul(valueFloat, priceFloat)
			usdValue, _ = result.Float64()
			log.Printf("🔍 Yeniden hesaplanan USD değeri: $%.2f", usdValue)
		}
	}

	// 2. Bilinen token'lar için ek kontroller
	if symbol, ok := tokenSymbols[strings.ToLower(tokenAddr.Hex())]; ok {
		switch symbol {
		case "USDC", "USDT":
			// USDC/USDT için 0.15$ = 461$ olmamalı
			if usdValue > 100 && usdValue < 1000 {
				log.Printf("⚠️ %s için şüpheli USD değeri: $%.2f, kontrol ediliyor", symbol, usdValue)
				// Fiyatı tekrar kontrol et
				price = fetchTokenUSDPrice(tokenAddr)
				if price > 0 {
					priceFloat = new(big.Float).SetFloat64(price)
					result = new(big.Float).Mul(valueFloat, priceFloat)
					usdValue, _ = result.Float64()
					log.Printf("🔍 %s için yeniden hesaplanan USD değeri: $%.2f", symbol, usdValue)
				}
			}
		}
	}

	// 3. Son güvenlik kontrolü: Çok büyük değerler için 0 döndür
	if usdValue > 1000000 {
		log.Printf("⚠️ Son güvenlik kontrolü: Çok büyük USD değeri $%.2f, 0 döndürülüyor", usdValue)
		return 0
	}

	log.Printf("🔍 USD değeri: %f", usdValue)

	return usdValue
}

// Çoklu kaynaklardan token USD fiyatı çek (DexScreener, CoinGecko, 1inch)
func fetchTokenUSDPrice(tokenAddr common.Address) float64 {
	addr := strings.ToLower(tokenAddr.Hex())
	if addr == "" || addr == "0x0000000000000000000000000000000000000000" {
		return 0
	}

	// Cache kontrolü
	if ent, ok := tokenPriceCache[addr]; ok {
		if time.Since(ent.cachedAt) < getTokenPriceTTL() {
			return ent.price
		}
	}

	// Çoklu kaynaklardan fiyat çek (fallback mekanizması ile)
	price := fetchFromMultipleSources(addr)

	// Stablecoin fiyat güvenliği: USDC/USDT için 1.0'a sabitle
	if sym, ok := tokenSymbols[addr]; ok && (sym == "USDC" || sym == "USDT") {
		if price < 0.9 || price > 1.1 {
			log.Printf("⚠️ Stablecoin %s için anormal fiyat: $%.4f → 1.0'a sabitleniyor", sym, price)
		}
		price = 1.0
	}

	if price > 0 {
		tokenPriceCache[addr] = tokenPriceEntry{price: price, cachedAt: time.Now()}
		log.Printf("🔍 Fiyat güncellendi: token=%s, fiyat=$%.4f", addr, price)
	}

	return price
}

// Çoklu kaynaklardan fiyat çek (fallback mekanizması ile)
func fetchFromMultipleSources(addr string) float64 {
	// Rate limiting kontrolü
	if isRateLimited() {
		log.Printf("⚠️ Rate limit aktif, cache'den fiyat kullanılıyor: %s", addr)
		return 0
	}

	// 1. DexScreener (ana kaynak - en güvenilir)
	if price := fetchFromDexScreener(addr); price > 0 {
		log.Printf("🔍 DexScreener'dan fiyat alındı: $%.4f", price)
		return price
	}

	// 2. CoinGecko (fallback - güvenilir)
	if price := fetchFromCoinGecko(addr); price > 0 {
		log.Printf("🔍 CoinGecko'dan fiyat alındı: $%.4f", price)
		return price
	}

	// 3. 1inch DEVRE DIŞI (güvenlik nedeniyle)
	// 1inch API'si yanlış fiyatlar döndürebiliyor, bu yüzden devre dışı bırakıldı
	log.Printf("⚠️ 1inch API devre dışı (güvenlik nedeniyle): %s", addr)

	log.Printf("⚠️ Hiçbir kaynaktan fiyat alınamadı: %s", addr)
	return 0
}

// Rate limiting kontrolü
var (
	lastRequestTime = time.Now()
	requestCount    = 0
	rateLimitMutex  sync.Mutex
)

func isRateLimited() bool {
	rateLimitMutex.Lock()
	defer rateLimitMutex.Unlock()

	now := time.Now()

	// 1 dakikada maksimum 30 istek (her API için 10 istek)
	if now.Sub(lastRequestTime) < time.Minute {
		if requestCount >= 30 {
			return true
		}
		requestCount++
	} else {
		// 1 dakika geçti, sayacı sıfırla
		lastRequestTime = now
		requestCount = 1
	}

	return false
}

// DexScreener'dan fiyat çek
func fetchFromDexScreener(addr string) float64 {
	url := fmt.Sprintf("https://api.dexscreener.com/latest/dex/tokens/%s", addr)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0
	}
	req = req.WithContext(context.Background())
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0
	}

	var payload struct {
		Pairs []struct {
			PriceUsd string `json:"priceUsd"`
			DexId    string `json:"dexId"`
		} `json:"pairs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0
	}

	best := 0.0
	bestDex := ""

	// En yüksek fiyatı bul
	for _, p := range payload.Pairs {
		if p.PriceUsd == "" {
			continue
		}
		if f, err := strconv.ParseFloat(p.PriceUsd, 64); err == nil && f > 0 {
			if f > best {
				best = f
				bestDex = p.DexId
			}
		}
	}

	if best > 0 {
		log.Printf("🔍 DexScreener: %s, fiyat=$%.4f, DEX=%s", addr, best, bestDex)
	}

	return best
}

// CoinGecko'dan fiyat çek (Arbitrum için)
func fetchFromCoinGecko(addr string) float64 {
	// CoinGecko Arbitrum token listesi
	url := "https://api.coingecko.com/api/v3/simple/token_price/arbitrum-one"
	params := fmt.Sprintf("?contract_addresses=%s&vs_currencies=usd", addr)

	req, err := http.NewRequest("GET", url+params, nil)
	if err != nil {
		return 0
	}
	req = req.WithContext(context.Background())
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0
	}

	var payload map[string]map[string]float64
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0
	}

	if tokenData, exists := payload[addr]; exists {
		if price, exists := tokenData["usd"]; exists && price > 0 {
			log.Printf("🔍 CoinGecko: %s, fiyat=$%.4f", addr, price)
			return price
		}
	}

	return 0
}

// ClearTokenPriceCache belirli bir token'ın fiyat cache'ini temizler
func ClearTokenPriceCache(tokenAddr common.Address) {
	addr := strings.ToLower(tokenAddr.Hex())
	delete(tokenPriceCache, addr)
	log.Printf("🔍 Fiyat cache temizlendi: %s", addr)
}

// ClearAllTokenPriceCache tüm fiyat cache'ini temizler
func ClearAllTokenPriceCache() {
	tokenPriceCache = make(map[string]tokenPriceEntry)
	log.Printf("🔍 Tüm fiyat cache temizlendi")
	log.Printf("🔒 Güvenlik: 1inch API devre dışı, sadece DexScreener ve CoinGecko kullanılıyor")
}

// ForceRefreshTokenPrice belirli bir token'ın fiyatını zorla yeniler
func ForceRefreshTokenPrice(tokenAddr common.Address) {
	addr := strings.ToLower(tokenAddr.Hex())
	delete(tokenPriceCache, addr)
	log.Printf("🔍 Token fiyatı zorla yenilendi: %s", addr)
}

// Token sembolleri (bilinen adresler)
var tokenSymbols = map[string]string{
	strings.ToLower("0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9"): "USDT",
	strings.ToLower("0x82aF49447D8a07e3bd95BD0d56f35241523fBab1"): "WETH",
	strings.ToLower("0x2f2a2543B76A4166549F7aaB2e75Bef0aefC5B0f"): "WBTC",
	strings.ToLower("0xaf88d065e77c8cC2239327C5EDb3A432268e5831"): "USDC", // Arbitrum USDC
	strings.ToLower("0xEA1523eB5F0ecDdB1875122aC2c9470a978e3010"): "USDC", // Eski USDC (geri uyumluluk)
	strings.ToLower("0xc5eFb9E4EfD91E68948d5039819494Eea56FFA46"): "PAXG",
}

// Token ondalıkları (bilinen adresler)
var tokenDecimals = map[string]int{
	strings.ToLower("0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9"): 6,  // USDT
	strings.ToLower("0x82aF49447D8a07e3bd95BD0d56f35241523fBab1"): 18, // WETH
	strings.ToLower("0x2f2a2543B76A4166549F7aaB2e75Bef0aefC5B0f"): 8,  // WBTC
	strings.ToLower("0xaf88d065e77c8cC2239327C5EDb3A432268e5831"): 6,  // Arbitrum USDC
	strings.ToLower("0xEA1523eB5F0ecDdB1875122aC2c9470a978e3010"): 6,  // Eski USDC (geri uyumluluk)
	strings.ToLower("0xc5eFb9E4EfD91E68948d5039819494Eea56FFA46"): 18, // PAXG
}

func getTokenDecimals(addr common.Address) int {
	addrLower := strings.ToLower(addr.Hex())
	if d, ok := tokenDecimals[addrLower]; ok {
		// Debug: USDC için decimal kontrolü
		if strings.EqualFold(addrLower, "0xaf88d065e77c8cc2239327c5edb3a432268e5831") ||
			strings.EqualFold(addrLower, "0xea1523eb5f0ecddb1875122ac2c9470a978e3010") {
			log.Printf("🔍 USDC decimal kontrolü: adres=%s, decimal=%d", addrLower, d)
		}
		// Debug: WBTC için decimal kontrolü
		if strings.EqualFold(addrLower, "0x2f2a2543b76a4166549f7aab2e75bef0aefc5b0f") {
			log.Printf("🔍 WBTC decimal kontrolü: adres=%s, decimal=%d", addrLower, d)
		}
		// Debug: PAXG için decimal kontrolü
		if strings.EqualFold(addrLower, "0xc5efb9e4efd91e68948d5039819494eea56ffa46") {
			log.Printf("🔍 PAXG decimal kontrolü: adres=%s, decimal=%d", addrLower, d)
		}
		return d
	}

	// Bilinmeyen token'lar için daha güvenli varsayılan
	// Çoğu ERC20 token 18 decimal kullanır, ama bazıları farklı olabilir
	log.Printf("⚠️ Bilinmeyen token decimal'ı, varsayılan 18 kullanılıyor: %s", addrLower)

	// Güvenlik: Bilinmeyen token'lar için fiyat hesaplamasını devre dışı bırak
	// Bu sayede yanlış fiyat hesaplamaları önlenir
	return 18
}

func getAssetSymbol(addr common.Address) string {
	if s, ok := tokenSymbols[strings.ToLower(addr.Hex())]; ok {
		return s
	}
	return ""
}

// formatWei wei değerini okunabilir formata çevirir
func formatWei(value *big.Int) string {
	if value.Cmp(big.NewInt(0)) == 0 {
		return "0"
	}

	// 18 decimal varsayarak
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

	// Tam sayı kısmı
	whole := new(big.Int).Div(value, divisor)

	// Ondalık kısım
	remainder := new(big.Int).Mod(value, divisor)

	if remainder.Cmp(big.NewInt(0)) == 0 {
		return whole.String()
	}

	// Ondalık kısmı string'e çevir ve padding ekle
	remainderStr := remainder.String()
	for len(remainderStr) < 18 {
		remainderStr = "0" + remainderStr
	}

	// Sondaki sıfırları kaldır
	for len(remainderStr) > 0 && remainderStr[len(remainderStr)-1] == '0' {
		remainderStr = remainderStr[:len(remainderStr)-1]
	}

	if len(remainderStr) == 0 {
		return whole.String()
	}

	return whole.String() + "." + remainderStr
}

// Genel amaçlı token miktarı formatlayıcı (decimals'e göre)
func formatTokenAmount(value *big.Int, decimals int) string {
	if value.Cmp(big.NewInt(0)) == 0 {
		return "0"
	}
	if decimals <= 0 {
		return value.String()
	}

	// Debug: USDC için detaylı log
	if decimals == 6 {
		log.Printf("🔍 USDC format debug: value=%s, decimals=%d", value.String(), decimals)
	}
	// Debug: WBTC için detaylı log
	if decimals == 8 {
		log.Printf("🔍 WBTC format debug: value=%s, decimals=%d", value.String(), decimals)
	}
	// Debug: PAXG için detaylı log
	if decimals == 18 {
		log.Printf("🔍 PAXG format debug: value=%s, decimals=%d", value.String(), decimals)
	}

	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	whole := new(big.Int).Div(value, divisor)
	remainder := new(big.Int).Mod(value, divisor)

	if decimals == 6 {
		log.Printf("🔍 USDC format debug: whole=%s, remainder=%s", whole.String(), remainder.String())
	}
	if decimals == 8 {
		log.Printf("🔍 WBTC format debug: whole=%s, remainder=%s", whole.String(), remainder.String())
	}
	if decimals == 18 {
		log.Printf("🔍 PAXG format debug: whole=%s, remainder=%s", whole.String(), remainder.String())
	}

	if remainder.Cmp(big.NewInt(0)) == 0 {
		return whole.String()
	}
	remainderStr := remainder.String()
	for len(remainderStr) < decimals {
		remainderStr = "0" + remainderStr
	}
	for len(remainderStr) > 0 && remainderStr[len(remainderStr)-1] == '0' {
		remainderStr = remainderStr[:len(remainderStr)-1]
	}
	if len(remainderStr) == 0 {
		return whole.String()
	}
	result := whole.String() + "." + remainderStr

	if decimals == 6 {
		log.Printf("🔍 USDC format debug: sonuç=%s", result)
	}
	if decimals == 8 {
		log.Printf("🔍 WBTC format debug: sonuç=%s", result)
	}
	if decimals == 18 {
		log.Printf("🔍 PAXG format debug: sonuç=%s", result)
	}

	return result
}

func handleLiveEvent(vLog types.Log) {
	// Yalnızca bizim adreslerle ilgili logları işle
	if !isRelevantLog(vLog) {
		return
	}

	// Native ETH transferini kontrol et (tx.Value>0 ve taraflardan biri biz)
	if vLog.TxHash != (common.Hash{}) {
		if titleN, bodyN, ok := tryBuildNativeTransferNotification(vLog); ok {
			select {
			case notificationBuffer <- notificationItem{title: titleN, body: bodyN, time: time.Now()}:
			default:
				log.Println("⚠️ Bildirim buffer'ı dolu, native ETH bildirimi atlandı")
			}
			return
		}
	}

	title, body := formatEventMessage(vLog)

	// Bildirimi buffer'a ekle
	select {
	case notificationBuffer <- notificationItem{
		title: title,
		body:  body,
		time:  time.Now(),
	}:
	default:
		// Buffer doluysa eski bildirimi at
		log.Println("⚠️ Bildirim buffer'ı dolu, eski bildirim atıldı")
	}
}

// isRelevantLog: yalnızca bizim adreslerle ilgili logları kabul eder
// - Transfer: from veya to bizim adreslerden biri olmalı
// - Diğer eventler: logu üreten kontrat bizim izlenen adreslerimizden biri olmalı
// - Zero address transfer logları: from veya to bizim adreslerden biri olmalı
func isRelevantLog(lg types.Log) bool {
	if len(lg.Topics) == 0 {
		return false
	}
	topic0 := lg.Topics[0]
	// Transfer eventleri: from/to kontrol et
	if topic0 == transferTopic {
		if len(lg.Topics) < 3 {
			return false
		}
		from := strings.ToLower(common.BytesToAddress(lg.Topics[1].Bytes()).Hex())
		to := strings.ToLower(common.BytesToAddress(lg.Topics[2].Bytes()).Hex())
		return WatchMap[from] || WatchMap[to]
	}
	// Diğer eventler: sadece bizim kontratlardan gelenleri kabul et
	return WatchMap[strings.ToLower(lg.Address.Hex())]
}

// removed: zero-address log tabanlı native tespit mantığı kaldırıldı (native log üretmez)

// tryBuildNativeTransferNotification: tx.Value>0 ise ve taraflardan biri bizim adrese eşitse bildirim üretir
func tryBuildNativeTransferNotification(lg types.Log) (string, string, bool) {
	// De-dupe: aynı tx için tekrar üretme
	txh := lg.TxHash.Hex()
	// removed: mutex no longer needed without parallel backfill
	if ts, ok := nativeSeen[txh]; ok {
		if time.Since(ts) < nativeSeenTTL {
			return "", "", false
		}
	}

	rpcUrl := os.Getenv("ARBITRUM_RPC")
	if strings.TrimSpace(rpcUrl) == "" {
		return "", "", false
	}
	client, err := ethclient.Dial(rpcUrl)
	if err != nil {
		return "", "", false
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	tx, _, err := client.TransactionByHash(ctx, lg.TxHash)
	if err != nil || tx == nil {
		return "", "", false
	}
	if tx.Value() == nil || tx.Value().Sign() <= 0 {
		return "", "", false
	}

	// To adresi
	toAddr := ""
	if tx.To() != nil {
		toAddr = strings.ToLower(tx.To().Hex())
	}

	// From adresi (imzadan çıkar)
	chainID, err := client.ChainID(ctx)
	if err != nil {
		return "", "", false
	}
	signer := types.LatestSignerForChainID(chainID)
	fromAddress, err := types.Sender(signer, tx)
	if err != nil {
		return "", "", false
	}
	fromAddr := strings.ToLower(fromAddress.Hex())

	// İlgililik: from/to bizim adreslerden biri olmalı
	isToWatched := toAddr != "" && WatchMap[toAddr]
	isFromWatched := WatchMap[fromAddr]
	if !isToWatched && !isFromWatched {
		return "", "", false
	}

	// Başlık/gövde
	cat := "ETH"
	emoji := "🔵"
	valueEth := new(big.Float).Quo(new(big.Float).SetInt(tx.Value()), new(big.Float).SetFloat64(1e18))
	valStr := valueEth.Text('f', 6)

	dir := ""
	if isFromWatched && isToWatched {
		dir = "internal"
	} else if isFromWatched {
		dir = "out"
	} else {
		dir = "in"
	}

	title := emoji + " [" + cat + "] Transfer (ETH)"
	body := fmt.Sprintf("📋 **Tx:** `%s`\n📤 **From:** `%s`\n📥 **To:** `%s`\n💰 **Value:** `%s ETH`\n🏷️ **Dir:** `%s`\n⏰ **Zaman:** `%s`",
		lg.TxHash.Hex(), fromAddr, toAddr, valStr, dir, time.Now().Format("02.01.2006 15:04:05"))

	nativeSeen[txh] = time.Now()
	return title, body, true
}

// startNotificationProcessor bildirimleri gruplandırır ve gönderir
func startNotificationProcessor() {
	notificationTicker = time.NewTicker(5 * time.Second) // 5 saniyede bir gruplandır

	go func() {
		var notifications []notificationItem

		for {
			select {
			case item := <-notificationBuffer:
				notifications = append(notifications, item)

				// İsteğe bağlı: önemli bildirimleri anında gönder (IMMEDIATE_IMPORTANT=true)
				if strings.ToLower(os.Getenv("IMMEDIATE_IMPORTANT")) == "true" {
					if strings.Contains(item.title, "🔴") || strings.Contains(item.body, "ÖZEL TRANSFER") ||
						strings.Contains(strings.ToLower(item.title), "diamondcut") || strings.Contains(strings.ToLower(item.body), "diamondcut→installmodule") ||
						strings.Contains(strings.ToLower(item.title), "installmodule") || strings.Contains(strings.ToLower(item.body), "installmodule") {
						sendGroupedNotifications(notifications)
						notifications = notifications[:0]
					}
				}

			case <-notificationTicker.C:
				if len(notifications) > 0 {
					sendGroupedNotifications(notifications)
					notifications = notifications[:0]
				}
			}
		}
	}()
}

// sendGroupedNotifications gruplandırılmış bildirimleri gönderir
func sendGroupedNotifications(notifications []notificationItem) {
	if len(notifications) == 0 {
		return
	}

	if len(notifications) == 1 {
		// Tek bildirim
		item := notifications[0]
		SendNotificationToAllNotifiers(item.title, item.body)
		return
	}

	// Eğer aralarında InstallModule/DiamondCut (InstallModule) varsa: her birini tek tek gönder (önemli olan ayrı düşsün)
	for _, it := range notifications {
		lt := strings.ToLower(it.title)
		lb := strings.ToLower(it.body)
		if strings.Contains(lt, "installmodule") || strings.Contains(lb, "installmodule") || strings.Contains(lt, "diamondcut") || strings.Contains(lb, "diamondcut→installmodule") {
			for _, single := range notifications {
				SendNotificationToAllNotifiers(single.title, single.body)
			}
			return
		}
	}

	// Çoklu bildirim
	title := fmt.Sprintf("📢 %d Yeni Event (%s)", len(notifications), time.Now().Format("15:04:05"))

	var body strings.Builder
	// Tarihi kod bloğunda ver, MarkdownV2 kaçış problemlerini azalt
	body.WriteString(fmt.Sprintf("⏰ `%s`\n\n", time.Now().Format("02.01.2006 15:04:05")))

	for i, item := range notifications {
		// Liste numarasındaki nokta (.) MarkdownV2 için kaçırılmalı
		body.WriteString(fmt.Sprintf("**%d\\.** %s\n%s\n\n", i+1, item.title, item.body))
	}

	SendNotificationToAllNotifiers(title, body.String())
}

func bootstrapScanWindowed(ctx context.Context, client *ethclient.Client) {
	blocksEnv := strings.TrimSpace(os.Getenv("BOOTSTRAP_BLOCKS"))
	if blocksEnv == "" {
		blocksEnv = "2000"
	}
	total, err := strconv.Atoi(blocksEnv)
	if err != nil || total <= 0 {
		total = 2000
	}

	maxWindow := 500
	if mwEnv := strings.TrimSpace(os.Getenv("BOOTSTRAP_MAX_WINDOW")); mwEnv != "" {
		if v, err := strconv.Atoi(mwEnv); err == nil && v > 0 {
			maxWindow = v
		}
	}

	head, err := client.BlockNumber(ctx)
	if err != nil {
		log.Printf("⚠️ Son blok numarası alınamadı: %v", err)
		return
	}
	if total > int(head) {
		total = int(head)
	}
	from := int64(head) - int64(total)
	if from < 0 {
		from = 0
	}

	log.Printf("🔁 Bootstrap taraması: toplam %d blok, pencere=%d", total, maxWindow)
	remaining := total
	cursor := int64(from)
	toHead := int64(head)

	for remaining > 0 {
		// Pencere boyutu (dahil aralık icin to=from+win-1)
		win := int(math.Min(float64(maxWindow), float64(remaining)))
		fromBlock := big.NewInt(cursor)
		toIncl := cursor + int64(win) - 1
		if toIncl > toHead {
			toIncl = toHead
		}
		toBlock := big.NewInt(toIncl)

		q := ethereum.FilterQuery{FromBlock: fromBlock, ToBlock: toBlock, Addresses: WatchAddresses}
		logs, err := client.FilterLogs(ctx, q)
		if err != nil {
			log.Printf("⚠️ Bootstrap penceresi (%d-%d) başarısız: %v", fromBlock.Int64(), toBlock.Int64(), err)
			time.Sleep(500 * time.Millisecond)
		} else if len(logs) > 0 {
			log.Printf("✅ Bootstrap penceresi (%d-%d): %d event", fromBlock.Int64(), toBlock.Int64(), len(logs))
			for _, lg := range logs {
				title, body := formatEventMessage(lg)
				// Bootstrap sırasında bildirimleri doğrudan göndermek yerine buffer'a koy
				if os.Getenv("BOOTSTRAP_NOTIFY") != "false" {
					notificationBuffer <- notificationItem{title: title, body: body, time: time.Now()}
				}
			}
		}

		processed := int(toBlock.Int64()-fromBlock.Int64()) + 1 // dahil aralik
		if processed <= 0 {
			break
		}
		remaining -= processed
		cursor += int64(processed)
	}
}

func subscribeWithReconnect(client *ethclient.Client) {
	query := ethereum.FilterQuery{Addresses: WatchAddresses}
	backoff := time.Second
	maxBackoff := 30 * time.Second

	trySubscribe := func() (ethereum.Subscription, chan types.Log, error) {
		logsCh := make(chan types.Log)
		sub, err := client.SubscribeFilterLogs(context.Background(), query, logsCh)
		return sub, logsCh, err
	}

	for {
		sub, logsCh, err := trySubscribe()
		if err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "notifications not supported") || strings.Contains(msg, "websocket") {
				log.Println("ℹ️ Subscribe desteklenmiyor, HTTP polling moduna geçiliyor...")
				pollLogs(context.Background(), client)
				time.Sleep(2 * time.Second)
				continue
			}
			log.Printf("❌ Subscribe hatası: %v", err)
			time.Sleep(backoff)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		log.Println("🔍 Event dinleme başlatıldı...")
		backoff = time.Second

		for {
			select {
			case err := <-sub.Err():
				log.Printf("⛔️ Dinleme hatası: %v", err)
				goto reconnect
			case vLog := <-logsCh:
				handleLiveEvent(vLog)
			}
		}

	reconnect:
		log.Println("↪️ Yeniden bağlanılıyor...")
	}
}

// İzlenen adresler için topics alanında kullanılacak 32-byte adres hash listesi
func buildWatchedAddressTopics() []common.Hash {
	topics := make([]common.Hash, 0, len(WatchAddresses))
	for _, a := range WatchAddresses {
		// topic alanları 32 byte; adresleri soldan sıfır ile pad edilmiş biçimde hash'e koyarız
		padded := common.LeftPadBytes(a.Bytes(), 32)
		topics = append(topics, common.BytesToHash(padded))
	}
	return topics
}

// ERC20 Transfer eventlerini dinler: topicIndex=1 (from) veya 2 (to) izlenen adreslerden biri
func subscribeTransferSideWithReconnect(client *ethclient.Client, topicIndex int) {
	if topicIndex != 1 && topicIndex != 2 {
		topicIndex = 1
	}
	watchedTopics := buildWatchedAddressTopics()
	buildQuery := func() ethereum.FilterQuery {
		// Topics: [transferTopic, from?, to?]; AND semantiği, aynı pozisyonda OR
		topics := make([][]common.Hash, 3)
		topics[0] = []common.Hash{transferTopic}
		if topicIndex == 1 {
			topics[1] = watchedTopics
			topics[2] = nil
		} else {
			topics[1] = nil
			topics[2] = watchedTopics
		}
		return ethereum.FilterQuery{Topics: topics}
	}

	backoff := time.Second
	maxBackoff := 30 * time.Second

	trySubscribe := func() (ethereum.Subscription, chan types.Log, error) {
		logsCh := make(chan types.Log)
		sub, err := client.SubscribeFilterLogs(context.Background(), buildQuery(), logsCh)
		return sub, logsCh, err
	}

	for {
		sub, logsCh, err := trySubscribe()
		if err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "notifications not supported") || strings.Contains(msg, "websocket") || strings.Contains(msg, "invalid logs options") {
				log.Println("ℹ️ Transfer subscribe desteklenmiyor, HTTP polling moduna geçiliyor...")
				pollTransfers(context.Background(), client, topicIndex)
				time.Sleep(2 * time.Second)
				continue
			}
			log.Printf("❌ Transfer subscribe hatası: %v", err)
			time.Sleep(backoff)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		log.Printf("🔍 Transfer dinleme başlatıldı (topicIndex=%d)...", topicIndex)
		backoff = time.Second

		for {
			select {
			case err := <-sub.Err():
				log.Printf("⛔️ Transfer dinleme hatası: %v", err)
				goto reconnect
			case vLog := <-logsCh:
				handleLiveEvent(vLog)
			}
		}

	reconnect:
		log.Println("↪️ Transfer dinleyici yeniden bağlanıyor...")
	}
}

// HTTP polling ile transferleri tarar (topicIndex=1: from, 2: to)
func pollTransfers(ctx context.Context, client *ethclient.Client, topicIndex int) {
	// Optimize edilmiş transfer polling: 5 saniye aralık, 100-300 blok aralığı
	interval := 5 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	head, err := client.BlockNumber(ctx)
	if err != nil {
		log.Printf("⚠️ Transfer polling başlangıç head alınamadı: %v", err)
		head = 0
	}
	last := head

	buildQuery := func(from, to *big.Int) ethereum.FilterQuery {
		watchedTopics := buildWatchedAddressTopics()
		topics := make([][]common.Hash, 3)
		topics[0] = []common.Hash{transferTopic}
		if topicIndex == 1 {
			topics[1] = watchedTopics
			topics[2] = nil
		} else {
			topics[1] = nil
			topics[2] = watchedTopics
		}
		return ethereum.FilterQuery{FromBlock: from, ToBlock: to, Topics: topics}
	}

	log.Printf("🧭 Optimize edilmiş transfer polling başlatıldı. Başlangıç head=%d, aralık=%s, blok aralığı=100-300 (topicIndex=%d)", last, interval, topicIndex)

	for {
		select {
		case <-ctx.Done():
			log.Println("🛑 Transfer polling durduruldu")
			return
		case <-ticker.C:
			cur, err := client.BlockNumber(ctx)
			if err != nil {
				log.Printf("⚠️ Transfer polling head alınamadı: %v", err)
				continue
			}
			if cur <= last {
				continue
			}

			// Blok aralığını hesapla (100-300 arası)
			blockRange := cur - last
			if blockRange > 300 {
				blockRange = 300 // Maksimum 300 blok
			} else if blockRange < 100 {
				// Eğer 100'den az blok varsa, biraz daha bekle
				continue
			}

			from := big.NewInt(int64(last + 1))
			to := big.NewInt(int64(last + blockRange))

			log.Printf("🔍 Transfer blok aralığı taranıyor: %d-%d (%d blok, topicIndex=%d)", from.Int64(), to.Int64(), blockRange, topicIndex)

			q := buildQuery(from, to)
			logs, err := client.FilterLogs(ctx, q)
			if err != nil {
				log.Printf("⚠️ Transfer polling log hatası (%d-%d): %v", from.Int64(), to.Int64(), err)
				continue
			}

			if len(logs) > 0 {
				log.Printf("📊 %d transfer event bulundu (%d-%d aralığında, topicIndex=%d)", len(logs), from.Int64(), to.Int64(), topicIndex)
			}

			for _, lg := range logs {
				handleLiveEvent(lg)
			}
			last = last + blockRange
		}
	}
}

// HTTP RPC üzerinde subscribe desteklenmiyorsa, periyodik olarak yeni blok aralığını tarar
func pollLogs(ctx context.Context, client *ethclient.Client) {
	// Optimize edilmiş polling: 3-5 saniye aralık, 100-300 blok aralığı
	interval := 4 * time.Second // 4 saniye aralık
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	head, err := client.BlockNumber(ctx)
	if err != nil {
		log.Printf("⚠️ Başlangıç blok alınamadı: %v", err)
		head = 0
	}
	last := head
	log.Printf("🧭 Optimize edilmiş polling başlatıldı. Başlangıç head=%d, aralık=%s, blok aralığı=100-300", last, interval)

	for {
		select {
		case <-ctx.Done():
			log.Println("🛑 Polling durduruldu")
			return
		case <-ticker.C:
			cur, err := client.BlockNumber(ctx)
			if err != nil {
				log.Printf("⚠️ Head alınamadı: %v", err)
				continue
			}
			if cur <= last {
				continue
			}

			// Blok aralığını hesapla (100-300 arası)
			blockRange := cur - last
			if blockRange > 300 {
				blockRange = 300 // Maksimum 300 blok
			} else if blockRange < 100 {
				// Eğer 100'den az blok varsa, biraz daha bekle
				continue
			}

			from := big.NewInt(int64(last + 1))
			to := big.NewInt(int64(last + blockRange))

			log.Printf("🔍 Blok aralığı taranıyor: %d-%d (%d blok)", from.Int64(), to.Int64(), blockRange)

			q := ethereum.FilterQuery{FromBlock: from, ToBlock: to, Addresses: WatchAddresses}
			logs, err := client.FilterLogs(ctx, q)
			if err != nil {
				log.Printf("⚠️ Polling log hatası (%d-%d): %v", from.Int64(), to.Int64(), err)
				continue
			}

			if len(logs) > 0 {
				log.Printf("📊 %d event bulundu (%d-%d aralığında)", len(logs), from.Int64(), to.Int64())
			}

			for _, lg := range logs {
				handleLiveEvent(lg)
			}
			last = last + blockRange
		}
	}
}

func StartEventListener() {
	rpcUrl := os.Getenv("ARBITRUM_RPC")
	if rpcUrl == "" {
		log.Fatal("❌ ARBITRUM_RPC ortam değişkeni tanımlı değil")
	}

	client, err := ethclient.Dial(rpcUrl)
	if err != nil {
		log.Fatalf("❌ RPC bağlantısı kurulamadı: %v", err)
	}
	fmt.Println("✅ RPC bağlantısı kuruldu")

	// Fiyat cache'ini temizle (güvenlik düzeltmeleri için)
	ClearAllTokenPriceCache()
	log.Println("🔒 Güvenlik kontrolleri aktif - 1inch API devre dışı")
	log.Println("🔄 Cache temizlendi, yeni fiyat hesaplama sistemi aktif")

	// Raw HTTP RPC client init (kalıcı, tx tiplerinden bağımsız)
	rawURL := strings.TrimSpace(os.Getenv("ARBITRUM_HTTP_RPC"))
	if rawURL == "" {
		// varsa wss/ws'yi https'ye çevir
		rawURL = strings.ReplaceAll(rpcUrl, "wss://", "https://")
		rawURL = strings.ReplaceAll(rawURL, "ws://", "http://")
	}
	if c, err := rpc.Dial(rawURL); err == nil {
		rawRPCClient = c
		log.Printf("🔗 Raw HTTP RPC hazır: %s", rawURL)
	} else {
		log.Printf("⚠️ Raw HTTP RPC kurulamadı: %v", err)
	}

	// Global event imzalarını yükle
	initGlobalEvents()

	// ABI'lerden event isimlerini yükle
	if err := LoadABIs(); err != nil {
		log.Printf("⚠️ ABI yükleme hatası: %v", err)
	}

	if id, err := client.ChainID(context.Background()); err == nil {
		log.Printf("🌐 ChainID: %s", id.String())
	}

	// Aktif profil bilgisini göster
	profile := strings.ToLower(strings.TrimSpace(os.Getenv("WALLET_PROFILE")))
	if profile == "test" {
		log.Printf("🧪 TEST PROFİLİ AKTİF")
	} else {
		log.Printf("🚀 PRODUCTION PROFİLİ AKTİF")
	}

	var addrSamples []string
	for i, a := range WatchAddresses {
		if i >= 5 {
			break
		}
		addrSamples = append(addrSamples, a.Hex())
	}
	log.Printf("👀 İzlenen adres sayısı: %d (örnekler: %s)", len(WatchAddresses), strings.Join(addrSamples, ", "))

	// Opsiyonel bootstrap taraması (env ile kontrol edilebilir)
	if strings.ToLower(os.Getenv("BOOTSTRAP_ENABLE")) != "false" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		go bootstrapScanWindowed(ctx, client)
	}

	// Opsiyonel: belirli bir tx hash'i için tanılama
	if diag := strings.TrimSpace(os.Getenv("DIAG_TX_HASH")); diag != "" {
		go func() {
			time.Sleep(500 * time.Millisecond)
			diagnoseTxByHash(client, diag)
		}()
	}

	// Native ETH tarayıcıyı başlat
	startNativeTxScanner(client)

	// Canlı event dinleme
	go subscribeWithReconnect(client)
	// ERC20 transferleri için hem from hem to tarafını ayrı dinle
	go subscribeTransferSideWithReconnect(client, 1)
	go subscribeTransferSideWithReconnect(client, 2)
	// Ana goroutine'i blokla
	select {}
}

// removed parallel backfill
// startNativeTxScanner: Yeni blokları tarayıp native ETH transferlerini (tx.Value>0) izler
// Yalnızca bizim adreslerimiz (from/to) ile ilgili işlemleri bildirir
func startNativeTxScanner(client *ethclient.Client) {
	go func() {
		// Native ETH tarayıcısını da optimize et: 3 saniye aralık
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		ctx := context.Background()
		var last uint64
		// Eğer bazı RPC sağlayıcıları yeni tx tiplerini desteklemiyorsa, kalıcı olarak raw moda geçeriz
		useRawOnly := false
		loggedRawSwitch := false
		// Başlangıç head + geri tarama
		if head, err := client.BlockNumber(ctx); err == nil {
			backfill := 0
			if v := strings.TrimSpace(os.Getenv("NATIVE_BACKFILL_BLOCKS")); v != "" {
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					backfill = n
				}
			}
			if backfill > 0 {
				if head > uint64(backfill) {
					last = head - uint64(backfill)
				} else {
					last = 0
				}
				log.Printf("🔙 Native backfill: son %d blok taranacak (başlangıç=%d)", backfill, last)
				// backfill kaldırıldı
			} else {
				last = head
			}
		}

		// Signer önbelleği
		var cachedChainID *big.Int
		var cachedSigner types.Signer
		debug := strings.ToLower(os.Getenv("DEBUG_MODE")) == "true"

		for range ticker.C {
			head, err := client.BlockNumber(ctx)
			if err != nil || head <= last {
				if debug {
					if err != nil {
						log.Printf("[native] head alinamadi: %v", err)
					} else {
						log.Printf("[native] yeni blok yok: head=%d last=%d", head, last)
					}
				}
				continue
			}
			for bnum := last + 1; bnum <= head; bnum++ {
				var blk *types.Block
				var gethErr error
				if !useRawOnly {
					blk, gethErr = client.BlockByNumber(ctx, new(big.Int).SetUint64(bnum))
				}
				if useRawOnly || gethErr != nil || blk == nil {
					// Eğer geth decode hatası "transaction type not supported" ise kalıcı olarak raw moda geç
					if gethErr != nil && strings.Contains(strings.ToLower(gethErr.Error()), "transaction type not supported") {
						useRawOnly = true
						if !loggedRawSwitch {
							log.Printf("[native] Geth decode desteklemiyor (yeni tx tipi). Kalıcı olarak RAW moda geçiliyor…")
							loggedRawSwitch = true
						}
					}
					if rawRPCClient == nil {
						// Raw client yoksa bu bloğu atla
						continue
					}
					var rawBlock struct {
						Transactions []struct {
							Hash  string `json:"hash"`
							From  string `json:"from"`
							To    string `json:"to"`
							Value string `json:"value"`
						} `json:"transactions"`
					}
					if err := rawRPCClient.CallContext(ctx, &rawBlock, "eth_getBlockByNumber", fmt.Sprintf("0x%x", bnum), true); err != nil {
						// Sessizce devam et (bazı sağlayıcılar bu endpointi kısıtlayabilir)
						continue
					}
					for _, rtx := range rawBlock.Transactions {
						val := new(big.Int)
						if len(rtx.Value) > 2 && strings.HasPrefix(rtx.Value, "0x") {
							if _, ok := val.SetString(rtx.Value[2:], 16); !ok {
								continue
							}
						}
						if val.Sign() <= 0 {
							continue
						}
						fromAddr := strings.ToLower(rtx.From)
						toAddr := strings.ToLower(rtx.To)
						isToWatched := toAddr != "" && WatchMap[toAddr]
						isFromWatched := WatchMap[fromAddr]
						if !isToWatched && !isFromWatched {
							continue
						}
						// De-dupe with mutex
						txh := rtx.Hash
						// removed: mutex no longer needed without parallel backfill
						if ts, ok := nativeSeen[txh]; ok {
							if time.Since(ts) < nativeSeenTTL {
								continue
							}
						}
						nativeSeen[txh] = time.Now()

						valueEth := new(big.Float).Quo(new(big.Float).SetInt(val), new(big.Float).SetFloat64(1e18))
						valStr := valueEth.Text('f', 6)
						dir := ""
						if isFromWatched && isToWatched {
							dir = "internal"
						} else if isFromWatched {
							dir = "out"
						} else {
							dir = "in"
						}
						// USD hesapla
						nativePrice := getNativeUSDPrice()
						ethUSD := new(big.Float).Mul(new(big.Float).SetFloat64(nativePrice), new(big.Float).Quo(new(big.Float).SetInt(val), new(big.Float).SetFloat64(1e18)))
						usdStr := func() string { f, _ := ethUSD.Float64(); return fmt.Sprintf("~$%.2f", f) }()
						// Receipt ve efektif gas fiyatı ekle
						rcpt, _ := client.TransactionReceipt(ctx, common.HexToHash(txh))
						effGas := ""
						status := ""
						if rcpt != nil {
							status = map[uint64]string{1: "success", 0: "reverted"}[rcpt.Status]
							if rcpt.EffectiveGasPrice != nil {
								gwei := new(big.Float).Quo(new(big.Float).SetInt(rcpt.EffectiveGasPrice), big.NewFloat(1e9))
								effGas = gwei.Text('f', 2) + " gwei"
							}
						}
						body := fmt.Sprintf("📋 **Tx:** `%s`\n📤 **From:** `%s`\n📥 **To:** `%s`\n💰 **Value:** `%s ETH`\n💵 **USD:** `%s`\n🏷️ **Dir:** `%s`\n📊 **Status:** `%s`\n⛽ **Gas:** `%s`\n⏰ **Zaman:** `%s`",
							txh, fromAddr, toAddr, valStr, usdStr, dir, status, effGas, time.Now().Format("02.01.2006 15:04:05"))
						// Emoji seçimi
						imp := determineImportance("[ETH] Transfer (ETH)", body)
						emoji := "🔵"
						if imp {
							emoji = "🔴"
						}
						title := emoji + " [ETH] Transfer (ETH)"
						select {
						case notificationBuffer <- notificationItem{title: title, body: body, time: time.Now()}:
						default:
							log.Println("⚠️ Bildirim buffer'ı dolu, native ETH bildirimi atlandı")
						}
					}
					continue
				}
				// Iterate txs (Geth yolu)
				for _, tx := range blk.Transactions() {
					if tx.Value() == nil || tx.Value().Sign() <= 0 {
						continue
					}
					toAddr := ""
					if tx.To() != nil {
						toAddr = strings.ToLower(tx.To().Hex())
					}
					if cachedSigner == nil {
						cid, cidErr := client.ChainID(ctx)
						if cidErr != nil {
							continue
						}
						cachedChainID = cid
						cachedSigner = types.LatestSignerForChainID(cachedChainID)
					}
					fromAddress, err := types.Sender(cachedSigner, tx)
					if err != nil {
						continue
					}
					fromAddr := strings.ToLower(fromAddress.Hex())
					isToWatched := toAddr != "" && WatchMap[toAddr]
					isFromWatched := WatchMap[fromAddr]
					if !isToWatched && !isFromWatched {
						continue
					}
					// De-dupe with mutex
					txh := tx.Hash().Hex()
					// removed: mutex no longer needed without parallel backfill
					if ts, ok := nativeSeen[txh]; ok {
						if time.Since(ts) < nativeSeenTTL {
							continue
						}
					}
					nativeSeen[txh] = time.Now()

					valueEth := new(big.Float).Quo(new(big.Float).SetInt(tx.Value()), new(big.Float).SetFloat64(1e18))
					valStr := valueEth.Text('f', 6)
					dir := ""
					if isFromWatched && isToWatched {
						dir = "internal"
					} else if isFromWatched {
						dir = "out"
					} else {
						dir = "in"
					}
					// USD hesapla
					nativePrice := getNativeUSDPrice()
					ethUSD := new(big.Float).Mul(new(big.Float).SetFloat64(nativePrice), new(big.Float).Quo(new(big.Float).SetInt(tx.Value()), new(big.Float).SetFloat64(1e18)))
					usdStr := func() string { f, _ := ethUSD.Float64(); return fmt.Sprintf("~$%.2f", f) }()
					// Receipt ve efektif gas fiyatı ekle
					rcpt, _ := client.TransactionReceipt(ctx, tx.Hash())
					effGas := ""
					status := ""
					if rcpt != nil {
						status = map[uint64]string{1: "success", 0: "reverted"}[rcpt.Status]
						if rcpt.EffectiveGasPrice != nil {
							gwei := new(big.Float).Quo(new(big.Float).SetInt(rcpt.EffectiveGasPrice), big.NewFloat(1e9))
							effGas = gwei.Text('f', 2) + " gwei"
						}
					}
					body := fmt.Sprintf("📋 **Tx:** `%s`\n📤 **From:** `%s`\n📥 **To:** `%s`\n💰 **Value:** `%s ETH`\n💵 **USD:** `%s`\n🏷️ **Dir:** `%s`\n📊 **Status:** `%s`\n⛽ **Gas:** `%s`\n⏰ **Zaman:** `%s`",
						txh, fromAddr, toAddr, valStr, usdStr, dir, status, effGas, time.Now().Format("02.01.2006 15:04:05"))
					// Emoji seçimi
					imp := determineImportance("[ETH] Transfer (ETH)", body)
					emoji := "🔵"
					if imp {
						emoji = "🔴"
					}
					title := emoji + " [ETH] Transfer (ETH)"
					select {
					case notificationBuffer <- notificationItem{title: title, body: body, time: time.Now()}:
					default:
						log.Println("⚠️ Bildirim buffer'ı dolu, native ETH bildirimi atlandı")
					}
				}
			}
			last = head
		}
	}()
}

// diagnoseTxByHash: Belirtilen işlem hash'i için native akış şartlarını adım adım kontrol eder
func diagnoseTxByHash(client *ethclient.Client, txHash string) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	h := common.HexToHash(txHash)
	tx, _, err := client.TransactionByHash(ctx, h)
	if err != nil || tx == nil {
		log.Printf("[diag] tx bulunamadı: %s err=%v", txHash, err)
		return
	}

	if tx.Value() == nil || tx.Value().Sign() <= 0 {
		log.Printf("[diag] tx value<=0, native degil: %s", txHash)
		return
	}

	// To
	toAddr := ""
	if tx.To() != nil {
		toAddr = strings.ToLower(tx.To().Hex())
	}

	// From
	chainID, err := client.ChainID(ctx)
	if err != nil {
		log.Printf("[diag] chainID alinamadi: %v", err)
		return
	}
	signer := types.LatestSignerForChainID(chainID)
	fromAddress, err := types.Sender(signer, tx)
	if err != nil {
		log.Printf("[diag] sender cikartilamadi: %v", err)
		return
	}
	fromAddr := strings.ToLower(fromAddress.Hex())

	// Relevance
	isToWatched := toAddr != "" && WatchMap[toAddr]
	isFromWatched := WatchMap[fromAddr]
	log.Printf("[diag] from=%s to=%s isFromWatched=%v isToWatched=%v", fromAddr, toAddr, isFromWatched, isToWatched)
	if !isToWatched && !isFromWatched {
		log.Printf("[diag] izlenen adreslerle iliskisiz")
		return
	}

	// Value str
	valueEth := new(big.Float).Quo(new(big.Float).SetInt(tx.Value()), new(big.Float).SetFloat64(1e18))
	valStr := valueEth.Text('f', 6)
	log.Printf("[diag] uygun: tx=%s val=%sETH dir=%s", txHash, valStr, func() string {
		if isFromWatched && isToWatched {
			return "internal"
		} else if isFromWatched {
			return "out"
		}
		return "in"
	}())
}
