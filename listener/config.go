package listener

import (
	"os"
	"strings"
)

// Event ismi örneğin "ModuleInstalled" ise
// .env'de "EVENT_MODULEINSTALLED=true" varsa true döner
func IsEventEnabled(eventName string) bool {
	key := "EVENT_" + strings.ToUpper(eventName)
	return strings.ToLower(os.Getenv(key)) == "true"
}
