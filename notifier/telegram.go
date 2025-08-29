package notifier

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Telegram struct {
	token      string
	chatID     string
	httpClient *http.Client

	// Basit per-chat rate limit: ~1 msg/sn
	mu          sync.Mutex
	nextAllowed time.Time
}

func NewTelegramFromEnv() (*Telegram, error) {
	token := strings.TrimSpace(strings.Trim(os.Getenv("TELEGRAM_BOT_TOKEN"), "\"'"))
	chatID := strings.TrimSpace(strings.Trim(os.Getenv("TELEGRAM_CHAT_ID"), "\"'"))
	if token == "" {
		return nil, errors.New("TELEGRAM_BOT_TOKEN boş")
	}
	if chatID == "" {
		return nil, errors.New("TELEGRAM_CHAT_ID boş")
	}
	return &Telegram{
		token:  token,
		chatID: chatID,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		nextAllowed: time.Now(),
	}, nil
}

type sendReq struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode,omitempty"` // "MarkdownV2" | "HTML"
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

// Rate limit bekleme: aynı chat için ~1.1s aralık
func (t *Telegram) waitRateLimit() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if now.Before(t.nextAllowed) {
		time.Sleep(t.nextAllowed.Sub(now))
	}
	t.nextAllowed = time.Now().Add(1100 * time.Millisecond)
}

func (t *Telegram) Notify(text string) error {
	payload := sendReq{
		ChatID:                t.chatID,
		Text:                  text,
		DisableWebPagePreview: true,
	}
	body, _ := json.Marshal(&payload)

	url := "https://api.telegram.org/bot" + t.token + "/sendMessage"

	var lastErr error
	backoff := 1200 * time.Millisecond // 1.2s başlangıç (rate limit ile uyumlu)
	maxAttempts := 5

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		t.waitRateLimit()

		req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := t.httpClient.Do(req)
		if err != nil {
			lastErr = err
		} else {
			func() {
				defer resp.Body.Close()
				if resp.StatusCode/100 == 2 {
					lastErr = nil
					return
				}
				// Hata gövdesini oku ve log'a taşıma için döndür
				b, _ := io.ReadAll(resp.Body)

				// 429 için Retry-After'a saygı duy
				if resp.StatusCode == http.StatusTooManyRequests {
					retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
					if retryAfter <= 0 {
						retryAfter = backoff
					}
					lastErr = errors.New("telegram 429 Too Many Requests: " + string(b))
					time.Sleep(retryAfter)
					// Bir sonraki denemede backoff'u arttır
					backoff = minDuration(backoff*2, 15*time.Second)
					return
				}

				// Diğer 4xx/5xx durumlar
				lastErr = errors.New("telegram sendMessage hata: " + resp.Status + " body=" + string(b))
			}()

			if lastErr == nil {
				return nil
			}
		}

		// 2xx değilse veya network hatasıysa: exponential backoff
		time.Sleep(backoff)
		backoff = minDuration(backoff*2, 15*time.Second)
	}

	return lastErr
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	// Saniye olarak gelebilir
	if sec, err := strconv.Atoi(v); err == nil && sec >= 0 {
		return time.Duration(sec) * time.Second
	}
	// HTTP-date ise kaba bir bekleme
	return 5 * time.Second
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
