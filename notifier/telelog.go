package notifier

import (
	"strings"
	"sync"
	"time"
)

// TelegramLogWriter implements io.Writer and forwards logs to Telegram.
// It batches frequent writes to reduce spam.
type TelegramLogWriter struct {
	telegram *Telegram
	queue    chan string
	once     sync.Once
}

// NewTelegramLogWriterFromEnv creates a TelegramLogWriter using env vars.
func NewTelegramLogWriterFromEnv() (*TelegramLogWriter, error) {
	tg, err := NewTelegramFromEnv()
	if err != nil {
		return nil, err
	}
	w := &TelegramLogWriter{
		telegram: tg,
		queue:    make(chan string, 100),
	}
	w.start()
	return w, nil
}

func (w *TelegramLogWriter) start() {
	w.once.Do(func() {
		go func() {
			var buffer []string
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case msg := <-w.queue:
					buffer = append(buffer, strings.TrimSpace(msg))
					if len(buffer) >= 10 {
						w.flush(&buffer)
					}
				case <-ticker.C:
					if len(buffer) > 0 {
						w.flush(&buffer)
					}
				}
			}
		}()
	})
}

func (w *TelegramLogWriter) flush(buf *[]string) {
	if len(*buf) == 0 {
		return
	}
	joined := strings.Join(*buf, "\n")
	_ = w.telegram.Notify("🧾 Log\n\n" + joined)
	*buf = (*buf)[:0]
}

// Write implements io.Writer. It is non-blocking when the queue is full.
func (w *TelegramLogWriter) Write(p []byte) (int, error) {
	select {
	case w.queue <- string(p):
	default:
		// Drop if queue is full to avoid blocking the app
	}
	return len(p), nil
}
