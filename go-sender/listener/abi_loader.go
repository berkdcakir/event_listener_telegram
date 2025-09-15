package listener

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// LoadABIs listener/abis klasöründen <address>.abi.json dosyalarını okuyup
// event topic0 -> event adı eşleşmelerini addressTopicNames tablosuna yazar.
func LoadABIs() error {
	baseDir := filepath.Join("listener", "abis")
	info, err := os.Stat(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // klasör yoksa sorun değil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s bir klasör değil", baseDir)
	}

	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !(strings.HasSuffix(strings.ToLower(name), ".json") || strings.HasSuffix(strings.ToLower(name), ".abi")) {
			return nil
		}
		// Dosya adı: <address>.abi.json veya <address>.json
		addrPart := strings.SplitN(name, ".", 2)[0]
		addr := strings.ToLower(addrPart)
		if !common.IsHexAddress(addr) {
			return nil
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var tmp any
		if err := json.Unmarshal(b, &tmp); err != nil {
			return fmt.Errorf("ABI JSON parse hatası (%s): %w", name, err)
		}
		abiStr := string(b)
		parsed, err := abi.JSON(strings.NewReader(abiStr))
		if err != nil {
			return fmt.Errorf("ABI parse hatası (%s): %w", name, err)
		}

		for evName, ev := range parsed.Events {
			id := ev.ID // topic0 hash alanı
			RegisterEventName(common.HexToAddress(addr), id, evName)
		}
		return nil
	}

	return filepath.WalkDir(baseDir, walkFn)
}
