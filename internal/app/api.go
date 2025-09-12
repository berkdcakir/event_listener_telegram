package app

import (
	"math"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"event-listener-backend/listener"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
)

// --- Simple idempotency for /notify by txHash (short TTL) ---
var seenTx = make(map[string]int64)

const seenTxTTLSeconds = 120

func markSeenTx(tx string) {
	now := time.Now().Unix()
	seenTx[tx] = now
	// prune simple
	for h, ts := range seenTx {
		if now-ts > seenTxTTLSeconds {
			delete(seenTx, h)
		}
	}
}

func isSeenTx(tx string) bool {
	if tx == "" {
		return false
	}
	now := time.Now().Unix()
	if ts, ok := seenTx[tx]; ok {
		if now-ts <= seenTxTTLSeconds {
			return true
		}
		delete(seenTx, tx)
	}
	return false
}

// SetupAPI HTTP API endpoint'lerini kurar
func SetupAPI() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// CORS middleware
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"service": "event-listener-backend",
		})
	})

	// Health alias
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Minimal notifier endpoint: Node watcher POST /notify
	r.POST("/notify", handleNotify)

	// Watch config (profil bazlı izlenecek adresler)
	r.GET("/config/watch", handleWatchConfig)

	// Balance endpoint'leri
	r.GET("/balance/:token", handleBalance)
	r.GET("/balance/main", handleMainBalance)
	r.GET("/balance/main/:token", handleMainTokenBalance)

	// Daily stats
	r.GET("/stats/daily", handleDailyStats)

	// TEST endpoints (sadece hızlı manuel doğrulama için)
	r.POST("/test/module-installed", handleTestModuleInstalled)

	return r
}

// handleBalance token balance'ını döner
func handleBalance(c *gin.Context) {
	token := strings.ToUpper(c.Param("token"))

	// Desteklenen token'ları kontrol et
	supportedTokens := map[string]bool{
		"USDT": true,
		"ETH":  true,
		"WBTC": true,
	}

	if !supportedTokens[token] {
		c.JSON(400, gin.H{
			"success": false,
			"error":   "Desteklenmeyen token. Desteklenen: USDT, ETH, WBTC",
		})
		return
	}

	// Balance'ı al
	balance, err := listener.GetTokenBalance(token)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(200, balance)
}

// handleMainBalance ana kontrat ETH balance'ını döner
func handleMainBalance(c *gin.Context) {
	balance, err := listener.GetMainBalance()
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(200, balance)
}

// handleMainTokenBalance ana kontratın token balance'ını döner
func handleMainTokenBalance(c *gin.Context) {
	token := strings.ToUpper(c.Param("token"))

	// Desteklenen token'ları kontrol et
	supportedTokens := map[string]bool{
		"USDT": true,
		"ETH":  true,
		"WBTC": true,
	}

	if !supportedTokens[token] {
		c.JSON(400, gin.H{
			"success": false,
			"error":   "Desteklenmeyen token. Desteklenen: USDT, ETH, WBTC",
		})
		return
	}

	balance, err := listener.GetMainTokenBalance(token)
	if err != nil {
		c.JSON(500, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(200, balance)
}

// handleDailyStats günlük istatistikleri döner
func handleDailyStats(c *gin.Context) {
	candidate := func(v string) string {
		s := strings.TrimSpace(v)
		if s == "" || strings.HasPrefix(s, "#") {
			return ""
		}
		return s
	}
	rpc := candidate(os.Getenv("ARBITRUM_RPC"))
	if rpc == "" {
		rpc = candidate(os.Getenv("ARBITRUM_HTTP_RPC"))
	}
	if rpc == "" {
		rpc = candidate(os.Getenv("RPC_HTTP"))
	}
	if rpc == "" {
		rpc = candidate(os.Getenv("ANKR_HTTP"))
	}
	if rpc == "" {
		c.JSON(500, gin.H{"success": false, "error": "RPC not set"})
		return
	}
	low := strings.ToLower(rpc)
	if strings.Contains(low, "rpc.ankr.com/arbitrum") && !strings.Contains(low, "/") {
		if key := candidate(os.Getenv("ANKR_API_KEY")); key != "" {
			rpc = strings.TrimRight(rpc, "/") + "/" + key
		}
	}
	client, err := ethclient.DialContext(c, rpc)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "error": err.Error()})
		return
	}
	defer client.Close()

	stats, err := listener.GetDailyStats(c, client)
	if err != nil {
		c.JSON(500, gin.H{"success": false, "error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": stats})
}

// handleTestModuleInstalled: ÖNEMLİ ModuleInstalled test bildirimi yollar
func handleTestModuleInstalled(c *gin.Context) {
	title := "🔴 [TEST] InstallModule"
	body := "📋 **Tx:** `0xTEST`\n🔧 **Modül:** `0xdeadbeef`\n⏰ **Zaman:** `" + listenerTimeNow() + "`"
	listener.SendNotificationToAllNotifiers(title, body)
	c.JSON(200, gin.H{"success": true})
}

// listenerTimeNow küçük bir yardımcı; format std ile aynı olsun
func listenerTimeNow() string {
	return strings.TrimSpace("" +
		// 02.01.2006 15:04:05 formatını listener ile aynı tutmak için
		// Go'da layout sabit olduğundan burada inline bırakıyoruz
		// net: bu fonksiyon sadece test endpoint'i için var
		// Not: importlarda already strings var
		// Zamanı listener tarafındaki format ile almak için küçük kısayol
		// Ancak burada doğrudan time.Now kullanamayız çünkü bu dosyada time importu yoktu
		// Kolay yol: listener tarafındaki formatla aynı olacak şekilde API body'i orada oluşturulsun
		"")
}

// handleNotify: Node viem watcher'dan gelen bildirimleri Telegram'a iletir
func handleNotify(c *gin.Context) {
	var payload struct {
		Type      string                 `json:"type"`
		Title     string                 `json:"title"`
		Addr      string                 `json:"addr"`
		Token     string                 `json:"token"`
		TxHash    string                 `json:"txHash"`
		Block     uint64                 `json:"block"`
		Timestamp int64                  `json:"timestamp"`
		Meta      map[string]interface{} `json:"meta"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	// idempotency: same txHash within TTL -> drop
	if payload.TxHash != "" && isSeenTx(payload.TxHash) {
		c.JSON(http.StatusOK, gin.H{"success": true, "duplicate": true})
		return
	}
	if payload.TxHash != "" {
		markSeenTx(payload.TxHash)
	}

	// Başlık: SendNotification tarafı başlığı zaten MarkdownV2 ile escape ediyor
	title := "🧭 " + strings.TrimSpace(payload.Title)
	b := &strings.Builder{}
	if payload.TxHash != "" {
		b.WriteString("🔗 Tx: `" + mdCode(payload.TxHash) + "`\n")
	}
	// Addr satırını kaldırdık - from/to zaten meta'da var
	// Token etiketini meta.tokenSymbol > meta.tokenAddress > payload.Token üzerinden türet
	tokenLabel := payload.Token
	if len(payload.Meta) > 0 {
		if ts, ok := payload.Meta["tokenSymbol"].(string); ok && ts != "" {
			t := strings.ToUpper(strings.TrimSpace(ts))
			if ta, ok := payload.Meta["tokenAddress"].(string); ok && ta != "" {
				low := strings.ToLower(ta)
				if low == strings.ToLower("0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9") {
					t = "USDT"
				}
				if low == strings.ToLower("0xaf88d065e77c8cC2239327C5EDb3A432268e5831") {
					t = "USDC"
				}
			}
			tokenLabel = t
		}
		if ta, ok := payload.Meta["tokenAddress"].(string); ok && ta != "" {
			if sym := listenerSymbolFromAddr(common.HexToAddress(ta)); sym != "" {
				tokenLabel = sym
			}
		}
	}
	if tokenLabel != "" {
		b.WriteString("🏷️ Token: `" + mdCode(tokenLabel) + "`\n")
	}
	if payload.Block != 0 {
		b.WriteString("⛓️ Block: `" + mdCode(fmtUint(payload.Block)) + "`\n")
	}
	if payload.Timestamp != 0 {
		b.WriteString("⏰ Ts: `" + mdCode(fmtInt(payload.Timestamp)) + "`\n")
	}

	// Token miktarı ve USDT karşılığını hesapla
	if len(payload.Meta) > 0 {
		// ERC20 için meta.value + meta.tokenAddress
		if valStr, ok := payload.Meta["value"].(string); ok && valStr != "" {
			tokenAddrStr, _ := payload.Meta["tokenAddress"].(string)
			amount, usdtValue := calculateErc20AmountAndUSDT(tokenAddrStr, valStr)
			if amount != "" {
				b.WriteString("💰 Amount: `" + mdCode(amount) + "`\n")
			}
			if usdtValue != "" {
				b.WriteString("💵 USDT: `" + mdCode(usdtValue) + "`\n")
			}
		}
		// Native için valueWei
		if valueWeiStr, ok := payload.Meta["valueWei"].(string); ok && valueWeiStr != "" {
			amount, usdtValue := calculateTokenAmountAndUSDT(payload.Token, valueWeiStr)
			if amount != "" {
				b.WriteString("💰 Amount: `" + mdCode(amount) + "`\n")
			}
			if usdtValue != "" {
				b.WriteString("💵 USDT: `" + mdCode(usdtValue) + "`\n")
			}
		}

		// From/To bilgilerini göster
		if from, ok := payload.Meta["from"].(string); ok && from != "" {
			b.WriteString("📤 From: `" + mdCode(from) + "`\n")
		}
		if to, ok := payload.Meta["to"].(string); ok && to != "" {
			b.WriteString("📥 To: `" + mdCode(to) + "`\n")
		}
		if dir, ok := payload.Meta["dir"].(string); ok && dir != "" {
			b.WriteString("🔄 Dir: `" + mdCode(dir) + "`\n")
		}
	}

	listener.SendNotificationToAllNotifiers(title, b.String())
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func fmtUint(v uint64) string { return strconv.FormatUint(v, 10) }
func fmtInt(v int64) string   { return strconv.FormatInt(v, 10) }

// mdCode escapes text for usage inside Telegram MarkdownV2 code formatting (`...`).
func mdCode(text string) string {
	// In code, only backticks and backslashes are problematic.
	s := strings.ReplaceAll(text, "\\", "\\\\")
	s = strings.ReplaceAll(s, "`", "\\`")
	return s
}

// calculateTokenAmountAndUSDT token miktarını ve USDT karşılığını hesaplar
func calculateTokenAmountAndUSDT(token, valueWeiStr string) (amount, usdtValue string) {
	// valueWei string'ini big.Int'e çevir
	valueWei, ok := new(big.Int).SetString(valueWeiStr, 10)
	if !ok {
		return "", ""
	}

	// Token decimal'ını belirle
	var decimals int
	tokenUpper := strings.ToUpper(token)
	switch tokenUpper {
	case "ETH":
		decimals = 18
	case "WETH":
		decimals = 18
	case "WBTC":
		decimals = 8
	case "USDT":
		decimals = 6
	case "USDC":
		decimals = 6
	default:
		decimals = 18 // Varsayılan
	}

	// Token miktarını hesapla
	divisor := new(big.Float).SetFloat64(math.Pow10(decimals))
	valueFloat := new(big.Float).SetInt(valueWei)
	valueFloat.Quo(valueFloat, divisor)

	// Formatla (6 ondalık basamak)
	amount = valueFloat.Text('f', 6)

	// Token sembolünü ekle
	amount = amount + " " + tokenUpper

	// USDT karşılığını hesapla
	if tokenUpper == "USDT" || tokenUpper == "USDC" {
		// Stablecoin'ler için 1:1
		usdtValue = amount + " USDT"
	} else {
		// Diğer tokenlar için fiyat hesapla
		tokenAddr := getTokenAddress(tokenUpper)
		if tokenAddr != (common.Address{}) {
			usdValue := listener.EstimateUSDValue(valueWei, tokenAddr)
			if usdValue > 0 {
				usdtValue = "$" + strconv.FormatFloat(usdValue, 'f', 2, 64) + " USDT"
			}
		}
	}

	return amount, usdtValue
}

// calculateErc20AmountAndUSDT: ERC20 transfer meta.value (token decimals) ve tokenAddress ile hesaplar
func calculateErc20AmountAndUSDT(tokenAddrStr, rawValueStr string) (amount, usdtValue string) {
	if tokenAddrStr == "" || rawValueStr == "" {
		return "", ""
	}
	tokenAddr := common.HexToAddress(tokenAddrStr)
	// rawValueStr BigInt (token'ın kendi decimals'ında)
	raw, ok := new(big.Int).SetString(rawValueStr, 10)
	if !ok {
		return "", ""
	}
	// Decimal bul (bilinen adresler)
	decimals := 18
	low := strings.ToLower(tokenAddr.Hex())
	switch low {
	case strings.ToLower("0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9"): // USDT
		decimals = 6
	case strings.ToLower("0xaf88d065e77c8cC2239327C5EDb3A432268e5831"): // USDC
		decimals = 6
	case strings.ToLower("0x2f2a2543B76A4166549F7aaB2e75Bef0aefC5B0f"): // WBTC
		decimals = 8
	case strings.ToLower("0x82aF49447D8a07e3bd95BD0d56f35241523fBab1"): // WETH
		decimals = 18
	case strings.ToLower("0xc5eFb9E4EfD91E68948d5039819494Eea56FFA46"): // PAXG
		decimals = 18
	case strings.ToLower("0xa0b862f60edef4452f25b4160f177db44deb6cf1"): // GNO
		decimals = 18
	}

	// Miktarı formatla
	div := new(big.Float).SetFloat64(math.Pow10(decimals))
	f := new(big.Float).SetInt(raw)
	f.Quo(f, div)
	amount = f.Text('f', 6) + " " + strings.ToUpper(listenerSymbolFromAddr(tokenAddr))

	usd := listener.EstimateUSDValue(raw, tokenAddr)
	if usd > 0 {
		usdtValue = "$" + strconv.FormatFloat(usd, 'f', 2, 64) + " USDT"
	}
	return amount, usdtValue
}

// listenerSymbolFromAddr: bilinen adres ise sembol, yoksa kısa adres döner
func listenerSymbolFromAddr(addr common.Address) string {
	s := listenerSymbolLookup(addr)
	if s != "" {
		return s
	}
	h := addr.Hex()
	if len(h) > 8 {
		return h[:6]
	}
	return h
}

// listenerSymbolLookup: listener içindeki token mapping'lerini dolaylı kullanmak için basit köprü
func listenerSymbolLookup(addr common.Address) string {
	// Bu fonksiyon doğrudan listener içindeki map'lere erişmiyor; sade fallback
	// Gerekirse burada sabit bilinenleri ekleyelim
	low := strings.ToLower(addr.Hex())
	switch low {
	case strings.ToLower("0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9"):
		return "USDT"
	case strings.ToLower("0x82aF49447D8a07e3bd95BD0d56f35241523fBab1"):
		return "WETH"
	case strings.ToLower("0x2f2a2543B76A4166549F7aaB2e75Bef0aefC5B0f"):
		return "WBTC"
	case strings.ToLower("0xaf88d065e77c8cC2239327C5EDb3A432268e5831"):
		return "USDC"
	case strings.ToLower("0xc5eFb9E4EfD91E68948d5039819494Eea56FFA46"):
		return "PAXG"
	case strings.ToLower("0xa0b862f60edef4452f25b4160f177db44deb6cf1"):
		return "GNO"
	default:
		return ""
	}
}

// getTokenAddress token sembolünden adres döner
func getTokenAddress(symbol string) common.Address {
	switch strings.ToUpper(symbol) {
	case "ETH", "WETH":
		return common.HexToAddress("0x82aF49447D8a07e3bd95BD0d56f35241523fBab1")
	case "WBTC":
		return common.HexToAddress("0x2f2a2543B76A4166549F7aaB2e75Bef0aefC5B0f")
	case "USDT":
		return common.HexToAddress("0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9")
	case "USDC":
		return common.HexToAddress("0xaf88d065e77c8cC2239327C5EDb3A432268e5831")
	case "PAXG":
		return common.HexToAddress("0xc5eFb9E4EfD91E68948d5039819494Eea56FFA46")
	case "GNO":
		return common.HexToAddress("0xa0b862f60edef4452f25b4160f177db44deb6cf1")
	default:
		return common.Address{}
	}
}

// handleWatchConfig: aktif WALLET_PROFILE'a göre izlenecek adresleri JSON döner
func handleWatchConfig(c *gin.Context) {
	// Env'e göre cüzdanları yükle (mevcutta main.go zaten yüklüyor; yine de güvenli tutalım)
	listener.LoadWalletsFromEnv()

	addrs := make([]string, 0, len(listener.WatchAddresses))
	for _, a := range listener.WatchAddresses {
		addrs = append(addrs, a.Hex())
	}

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"profile":        os.Getenv("WALLET_PROFILE"),
		"watchAddresses": addrs,
	})
}
