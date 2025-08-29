package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// TelegramBot Telegram bot işlemleri için
type TelegramBot struct {
	token           string
	httpClient      *http.Client
	apiBase         string
	processedMsgIDs map[int]time.Time
	mu              sync.Mutex
}

// Update Telegram webhook update
type Update struct {
	UpdateID int     `json:"update_id"`
	Message  Message `json:"message"`
}

// Message Telegram message
type Message struct {
	MessageID int    `json:"message_id"`
	From      User   `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

// User Telegram user
type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

// Chat Telegram chat
type Chat struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
}

// NewTelegramBot yeni bot instance'ı oluşturur
func NewTelegramBot() (*TelegramBot, error) {
	token := strings.TrimSpace(strings.Trim(os.Getenv("TELEGRAM_BOT_TOKEN"), "\"'"))
	if token == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN boş")
	}

	return &TelegramBot{
		token: token,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		apiBase:         "https://api.telegram.org/bot" + token,
		processedMsgIDs: make(map[int]time.Time),
	}, nil
}

// SendMessage mesaj gönderir
func (t *TelegramBot) SendMessage(chatID int, text string) error {
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "MarkdownV2",
	}

	body, _ := json.Marshal(payload)
	url := t.apiBase + "/sendMessage"

	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API hatası: %s - %s", resp.Status, string(b))
	}

	return nil
}

// SendMessageWithKeyboard: parse_mode olmadan, tıklanabilir Reply Keyboard ile mesaj gönderir
func (t *TelegramBot) SendMessageWithKeyboard(chatID int, text string, keyboard [][]string) error {
	// Telegram ReplyKeyboardMarkup formatına uygun nesne kur
	type button struct {
		Text string `json:"text"`
	}
	// 2D string -> 2D button
	kb := make([][]button, 0, len(keyboard))
	for _, row := range keyboard {
		r := make([]button, 0, len(row))
		for _, txt := range row {
			r = append(r, button{Text: txt})
		}
		kb = append(kb, r)
	}

	replyMarkup := map[string]interface{}{
		"keyboard":          kb,
		"resize_keyboard":   true,
		"one_time_keyboard": false,
		"is_persistent":     true,
		"selective":         false,
	}

	payload := map[string]interface{}{
		"chat_id":      chatID,
		"text":         text,
		"reply_markup": replyMarkup,
	}

	body, _ := json.Marshal(payload)
	url := t.apiBase + "/sendMessage"

	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API hatası: %s - %s", resp.Status, string(b))
	}

	return nil
}

// GetUpdates webhook updates'leri alır
func (t *TelegramBot) GetUpdates(offset int) ([]Update, error) {
	url := fmt.Sprintf("%s/getUpdates?offset=%d&timeout=50", t.apiBase, offset)

	resp, err := t.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("telegram API hatası: %s - %s", resp.Status, string(b))
	}

	var result struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if !result.OK {
		return nil, fmt.Errorf("telegram API başarısız")
	}

	return result.Result, nil
}

// HandleCommand komutları işler
func (t *TelegramBot) HandleCommand(chatID int, command string, messageID int) error {
	// duplicate koruması
	if !t.shouldProcess(messageID) {
		return nil
	}
	lc := strings.ToLower(strings.TrimSpace(command))

	switch lc {
	case "/help":
		return t.sendHelpMessage(chatID)
	case "/balanceusdt":
		return t.sendBalanceMessage(chatID, "USDT")
	case "/balanceeth":
		return t.sendBalanceMessage(chatID, "ETH")
	case "/balancewbtc":
		return t.sendBalanceMessage(chatID, "WBTC")
	case "/balancemain":
		return t.sendMainBalanceMessage(chatID)
	case "/mainusdt":
		return t.sendMainTokenBalanceMessage(chatID, "USDT")
	case "/maineth":
		return t.sendMainTokenBalanceMessage(chatID, "ETH")
	case "/mainwbtc":
		return t.sendMainTokenBalanceMessage(chatID, "WBTC")
	case "/dailystats":
		return t.sendDailyStats(chatID)
	default:
		// Komut değilse: teşekkür algıla
		if strings.Contains(lc, "teşekkür") || strings.Contains(lc, "tesekkur") || strings.Contains(lc, "tesekkür") {
			return t.SendMessage(chatID, formatBold("😊 Rica ederim"))
		}
		// Fenerbahçe algıla
		if strings.Contains(lc, "fenerbahçe") || strings.Contains(lc, "fenerbahce") {
			return t.SendMessage(chatID, formatBold("🐥 SEN ÇOK YAŞA"))
		}
		// Galatasaray algıla
		if strings.Contains(lc, "galatasaray") {
			return t.SendMessage(chatID, formatBold("🦁 MAURO ICARDIIIII"))
		}
		// Bilinmeyen mesajları yanıtsız bırak
		return nil
	}
}

// shouldProcess: aynı Telegram message_id'yi kısa süre içinde ikinci kez işleme
func (t *TelegramBot) shouldProcess(messageID int) bool {
	if messageID == 0 {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if ts, ok := t.processedMsgIDs[messageID]; ok {
		if time.Since(ts) < 2*time.Minute {
			return false
		}
	}
	t.processedMsgIDs[messageID] = time.Now()

	// basit temizlik
	if len(t.processedMsgIDs) > 1000 {
		cutoff := time.Now().Add(-5 * time.Minute)
		for id, t0 := range t.processedMsgIDs {
			if t0.Before(cutoff) {
				delete(t.processedMsgIDs, id)
			}
		}
	}
	return true
}

// sendHelpMessage yardım mesajı gönderir
func (t *TelegramBot) sendHelpMessage(chatID int) error {
	// Reply Keyboard butonları
	keyboard := [][]string{
		{"/balanceUsdt", "/balanceEth", "/balanceWbtc"},
		{"/balanceMain", "/mainUsdt", "/mainEth", "/mainWbtc"},
		{"/dailyStats", "/help"},
	}

	// Düz metin (parse_mode yok) – tıklanabilir butonlar aktif
	helpText := "Mevcut Komutlar\n\n" +
		"Hub Balanceleri:\n" +
		"/balanceUsdt - USDT Hub balance'ını gösterir\n" +
		"/balanceEth - ETH Hub balance'ını gösterir\n" +
		"/balanceWbtc - WBTC Hub balance'ını gösterir\n\n" +
		"Ana Kontrat Balanceleri:\n" +
		"/balanceMain - Ana kontrat ETH balance'ını gösterir\n" +
		"/mainUsdt - Ana kontrat USDT balance'ını gösterir\n" +
		"/mainEth - Ana kontrat ETH balance'ını gösterir\n" +
		"/mainWbtc - Ana kontrat WBTC balance'ını gösterir\n\n" +
		"Günlük Değişimler:\n" +
		"/dailyStats - 24 saatteki işlem sayısı ve balance değişimleri"

	return t.SendMessageWithKeyboard(chatID, helpText, keyboard)
}

// sendBalanceMessage token balance mesajı gönderir
func (t *TelegramBot) sendBalanceMessage(chatID int, token string) error {
	// Backend API'ye istek at
	apiURL := os.Getenv("BACKEND_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}

	url := fmt.Sprintf("%s/balance/%s", apiURL, strings.ToLower(token))

	resp, err := t.httpClient.Get(url)
	if err != nil {
		return t.SendMessage(chatID, fmt.Sprintf("API bağlantı hatası: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return t.SendMessage(chatID, fmt.Sprintf("API hatası: %s", resp.Status))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return t.SendMessage(chatID, fmt.Sprintf("JSON parse hatası: %v", err))
	}

	if success, ok := result["success"].(bool); !ok || !success {
		errorMsg := "Bilinmeyen hata"
		if errStr, ok := result["error"].(string); ok {
			errorMsg = errStr
		}
		return t.SendMessage(chatID, fmt.Sprintf("Balance alınamadı: %s", errorMsg))
	}

	balance := result["balance"].(string)
	address := result["address"].(string)
	symbol := result["symbol"].(string)

	message := formatBold(fmt.Sprintf("💰 %s Hub Balance", symbol)) + "\n\n" +
		formatBold("🏦 Hub:") + " " + formatCode(address) + "\n" +
		formatBold("💵 Balance:") + " " + formatBold(fmt.Sprintf("%s %s", balance, symbol)) + "\n\n" +
		formatBold("📊 Güncel zaman:") + " " + formatCode(time.Now().Format("02.01.2006 15:04:05"))

	return t.SendMessage(chatID, message)
}

// sendMainBalanceMessage ana kontrat balance mesajı gönderir
func (t *TelegramBot) sendMainBalanceMessage(chatID int) error {
	// Backend API'ye istek at
	apiURL := os.Getenv("BACKEND_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}

	url := fmt.Sprintf("%s/balance/main", apiURL)

	resp, err := t.httpClient.Get(url)
	if err != nil {
		return t.SendMessage(chatID, fmt.Sprintf("API bağlantı hatası: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return t.SendMessage(chatID, fmt.Sprintf("API hatası: %s", resp.Status))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return t.SendMessage(chatID, fmt.Sprintf("JSON parse hatası: %v", err))
	}

	if success, ok := result["success"].(bool); !ok || !success {
		errorMsg := "Bilinmeyen hata"
		if errStr, ok := result["error"].(string); ok {
			errorMsg = errStr
		}
		return t.SendMessage(chatID, fmt.Sprintf("Balance alınamadı: %s", errorMsg))
	}

	balance := result["balance"].(string)
	address := result["address"].(string)
	symbol := result["symbol"].(string)

	message := formatBold("🏦 Ana Kontrat Balance") + "\n\n" +
		formatBold("📋 Kontrat:") + " " + formatCode(address) + "\n" +
		formatBold("💵 Balance:") + " " + formatBold(fmt.Sprintf("%s %s", balance, symbol)) + "\n\n" +
		formatBold("📊 Güncel zaman:") + " " + formatCode(time.Now().Format("02.01.2006 15:04:05"))

	return t.SendMessage(chatID, message)
}

// sendMainTokenBalanceMessage ana kontratın token balance mesajı gönderir
func (t *TelegramBot) sendMainTokenBalanceMessage(chatID int, token string) error {
	// Backend API'ye istek at
	apiURL := os.Getenv("BACKEND_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}

	url := fmt.Sprintf("%s/balance/main/%s", apiURL, strings.ToLower(token))

	resp, err := t.httpClient.Get(url)
	if err != nil {
		return t.SendMessage(chatID, fmt.Sprintf("API bağlantı hatası: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return t.SendMessage(chatID, fmt.Sprintf("API hatası: %s", resp.Status))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return t.SendMessage(chatID, fmt.Sprintf("JSON parse hatası: %v", err))
	}

	if success, ok := result["success"].(bool); !ok || !success {
		errorMsg := "Bilinmeyen hata"
		if errStr, ok := result["error"].(string); ok {
			errorMsg = errStr
		}
		return t.SendMessage(chatID, fmt.Sprintf("Balance alınamadı: %s", errorMsg))
	}

	balance := result["balance"].(string)
	address := result["address"].(string)
	symbol := result["symbol"].(string)

	message := formatBold(fmt.Sprintf("🏦 Ana Kontrat %s Balance", symbol)) + "\n\n" +
		formatBold("📋 Kontrat:") + " " + formatCode(address) + "\n" +
		formatBold("💵 Balance:") + " " + formatBold(fmt.Sprintf("%s %s", balance, symbol)) + "\n\n" +
		formatBold("📊 Güncel zaman:") + " " + formatCode(time.Now().Format("02.01.2006 15:04:05"))

	return t.SendMessage(chatID, message)
}

func (t *TelegramBot) sendDailyStats(chatID int) error {
	apiURL := os.Getenv("BACKEND_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}
	url := fmt.Sprintf("%s/stats/daily", apiURL)
	resp, err := t.httpClient.Get(url)
	if err != nil {
		return t.SendMessage(chatID, fmt.Sprintf("API bağlantı hatası: %v", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return t.SendMessage(chatID, fmt.Sprintf("API hatası: %s", resp.Status))
	}
	var out struct {
		Success bool `json:"success"`
		Data    struct {
			FromBlock uint64 `json:"fromBlock"`
			ToBlock   uint64 `json:"toBlock"`
			FromTs    int64  `json:"fromTs"`
			ToTs      int64  `json:"toTs"`
			Items     []struct {
				Label          string `json:"label"`
				Address        string `json:"address"`
				Symbol         string `json:"symbol"`
				Tx24h          int    `json:"tx24h"`
				Change24h      string `json:"change24h"`
				CurrentBalance string `json:"currentBalance"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return t.SendMessage(chatID, fmt.Sprintf("JSON parse hatası: %v", err))
	}
	if !out.Success {
		return t.SendMessage(chatID, "İstatistik alınamadı")
	}
	b := &strings.Builder{}
	b.WriteString(formatBold("📊 Günlük İstatistikler (24h)") + "\n\n")
	for _, it := range out.Data.Items {
		fmt.Fprintf(b, "%s\n%s: %s\n%s: %d\n%s: %s\n%s: %s %s\n\n",
			formatBold(it.Label),
			formatBold("📍 Adres"), formatCode(it.Address),
			formatBold("📈 Tx"), it.Tx24h,
			formatBold("📊 Değişim"), escapeMarkdownV2(it.Change24h),
			formatBold("💵 Güncel"), escapeMarkdownV2(it.CurrentBalance), escapeMarkdownV2(it.Symbol))
	}
	return t.SendMessage(chatID, b.String())
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

// formatBold kalın metin formatı
func formatBold(text string) string {
	return "*" + escapeMarkdownV2(text) + "*"
}

// formatCode kod formatı
func formatCode(text string) string {
	return "`" + escapeMarkdownV2(text) + "`"
}
