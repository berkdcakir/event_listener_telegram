package notifier

// Notifier interface - birden fazla bildirim kanalı için
type Notifier interface {
	Notify(text string) error
}
