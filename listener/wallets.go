package listener

import (
	"log"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// Profil tanımı
type walletEntry struct {
	addr  string
	label string
}

// Production cüzdanları
var prodWallets = []walletEntry{
	{addr: "0x33381eC82DD811b1BABa841f1e2410468aeD7047", label: "Main App"},
	{addr: "0x845A66F0230970971240d76fdDF7f961e08e3f01", label: "wETH Hub"},
	{addr: "0x3b0794015C9595aE06cf2069C0faC5d9B290f911", label: "USDT Hub"},
	{addr: "0xec6595E48933D6f752a6f6421f0a9A019Fb80081", label: "wBTC Hub"},
	{addr: "0xEA1523eB5F0ecDdB1875122aC2c9470a978e3010", label: "USDC Hub"},
	{addr: "0xc5eFb9E4EfD91E68948d5039819494Eea56FFA46", label: "PAXG Hub"},
	{addr: "0xdAE486e75Cdf40bd9B2A0086dCf66e2d6C4e784b", label: "PECTO Hub"},
}

// Test cüzdanları
var testWallets = []walletEntry{
	{addr: "0x7A058060dD1C45eF6c79B36C1555655830f3B4AC", label: "Main App"},
	{addr: "0x5ee7E95d40258516fe198c22D987A82930dC1D03", label: "wETH Hub"},
	{addr: "0x569d561965e85C68222C2caC5E241Bc8647E431d", label: "USDT Hub"},
	{addr: "0xe0Fa88e388f27750Ce5519600cC01651f973abfA", label: "wBTC Hub"},
	{addr: "0xddE03B3aaA0d1390BD19AA6EF58Eb7F15a2a4B25", label: "USDC Hub"},
	{addr: "0x47556c13DBAEFB9CeCc9912C3921acE13fdCAC55", label: "PAXG Hub"},
	{addr: "0xf613f5BaA4Ca549D848c391f77E939A6774E8589", label: "Suleman"}, // suleman abenin wallet
	{addr: "0x015FC372F9207d041FbA3a00101f99420CaaD77A", label: "User Wallet"},
}

// Aktif izlenecek adresler
var WatchAddresses []common.Address

// İzleme kontrolü için map versiyonu (adres eşleşmesi için ideal)
var WatchMap map[string]bool

// Adres -> kategori etiketi (mesajda kullanılacak)
var AddressCategory map[string]string

// Testte yalnızca belirli EOA cüzdanlarını süzmek için (örn. Suleman)
var SpecialTestWallets map[string]bool

func init() {
	// Başlangıçta boş; env yüklendikten sonra LoadWalletsFromEnv çağrılacak
	WatchAddresses = make([]common.Address, 0)
	WatchMap = make(map[string]bool)
	AddressCategory = make(map[string]string)
	SpecialTestWallets = make(map[string]bool)
}

// LoadWallets verilen profil adına göre adresleri yükler
func LoadWallets(profile string) {
	p := strings.ToLower(strings.TrimSpace(profile))
	log.Printf("🔍 WALLET_PROFILE env değeri: '%s'", os.Getenv("WALLET_PROFILE"))
	log.Printf("🔍 İşlenmiş profil: '%s'", p)

	var chosen []walletEntry
	if p == "test" {
		chosen = testWallets
		log.Printf("✅ Test cüzdanları seçildi")
	} else {
		chosen = prodWallets
		log.Printf("✅ Production cüzdanları seçildi")
	}

	WatchAddresses = make([]common.Address, 0, len(chosen))
	WatchMap = make(map[string]bool, len(chosen))
	AddressCategory = make(map[string]string, len(chosen))
	// Özel test cüzdan filtresi sıfırla
	SpecialTestWallets = make(map[string]bool)

	for _, w := range chosen {
		addr := common.HexToAddress(w.addr)
		WatchAddresses = append(WatchAddresses, addr)
		lower := strings.ToLower(addr.Hex())
		WatchMap[lower] = true
		AddressCategory[lower] = w.label
	}

	// Ortam değişkeniyle ek izlenecek adresler (virgül ayrılmış)
	// Örn: WATCH_EXTRA_ADDRESSES=0xabc...,0xdef...
	if extra := strings.TrimSpace(os.Getenv("WATCH_EXTRA_ADDRESSES")); extra != "" {
		parts := strings.Split(extra, ",")
		for _, raw := range parts {
			addrStr := strings.TrimSpace(raw)
			if addrStr == "" {
				continue
			}
			// Geçerli hex adresine dönüştür
			addr := common.HexToAddress(addrStr)
			WatchAddresses = append(WatchAddresses, addr)
			lower := strings.ToLower(addr.Hex())
			WatchMap[lower] = true
			if _, ok := AddressCategory[lower]; !ok {
				AddressCategory[lower] = "Extra"
			}
		}
		log.Printf("➕ WATCH_EXTRA_ADDRESSES ile %d ekstra adres eklendi", len(WatchAddresses)-len(chosen))
	}

	// Native 0x transfer log'ları ERC-20 mint/burn içindir; native coin için log üretmez.
	// Bu nedenle zero address dinlemesini varsayılan olarak kapalı tutuyoruz.
	if strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_NATIVE_ZERO_TRANSFER_LOGS"))) == "true" {
		zero := common.Address{} // 0x000...000
		WatchAddresses = append(WatchAddresses, zero)
		log.Printf("🔧 Zero address log izlemesi aktif (ENABLE_NATIVE_ZERO_TRANSFER_LOGS=true)")
	}

	// Sadece bizim adresler dinlenecek; token kontratları eklenmeyecek.

	// Test profilinde yalnızca Süleman cüzdanını özel filtreye ekle (gürültüyü azaltmak için)
	if p == "test" {
		for _, w := range chosen {
			if strings.EqualFold(w.label, "Suleman") { // suleman abenin wallet
				SpecialTestWallets[strings.ToLower(w.addr)] = true
			}
		}
	}

	if p == "test" {
		log.Printf("🧪 Test profili aktif (%d adres)", len(chosen))
	} else {
		log.Printf("🚀 Production profili aktif (%d adres)", len(chosen))
	}
}

// Çalışma anında programatik olarak adres eklemek için yardımcı
func AddWatchedAddress(addr common.Address, label string) {
	lower := strings.ToLower(addr.Hex())
	if !WatchMap[lower] {
		WatchAddresses = append(WatchAddresses, addr)
	}
	WatchMap[lower] = true
	if strings.TrimSpace(label) == "" {
		label = "Extra"
	}
	AddressCategory[lower] = label
}

// LoadWalletsFromEnv env'den profili okuyup cüzdanları yükler
func LoadWalletsFromEnv() {
	LoadWallets(os.Getenv("WALLET_PROFILE"))
}

// LogActiveWallets aktif adresleri ve etiketlerini loglar
func LogActiveWallets() {
	if len(WatchAddresses) == 0 {
		log.Printf("⚠️ İzlenecek adres yok. Önce LoadWalletsFromEnv çağrılmalı.")
		return
	}
	for _, a := range WatchAddresses {
		lower := strings.ToLower(a.Hex())
		label := AddressCategory[lower]
		log.Printf("🔎 %s: %s", label, a.Hex())
	}
}

func GetAddressCategory(addr common.Address) string {
	if v, ok := AddressCategory[strings.ToLower(addr.Hex())]; ok {
		return v
	}
	return "General"
}

// Sade kategori etiketi
func GetCategoryLabel(addr common.Address) string {
	return GetAddressCategory(addr)
}
