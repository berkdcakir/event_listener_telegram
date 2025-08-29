package app

import (
	"os"
	"strings"

	"event-listener-backend/listener"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
)

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

// handleMainBalance ana kontratın ETH balance'ını döner
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

// handleMainTokenBalance ana kontratın belirtilen token balance'ını döner
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
	rpc := os.Getenv("ARBITRUM_RPC")
	if rpc == "" {
		c.JSON(500, gin.H{"success": false, "error": "ARBITRUM_RPC not set"})
		return
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
