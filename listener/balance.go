package listener

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Token kontratları
var TokenContracts = map[string]common.Address{
	"USDT": common.HexToAddress("0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9"), // Arbitrum USDT
	"ETH":  common.HexToAddress("0x82aF49447D8a07e3bd95BD0d56f35241523fBab1"), // Arbitrum WETH
	"WBTC": common.HexToAddress("0x2f2a2543B76A4166549F7aaB2e75Bef0aefC5B0f"), // Arbitrum WBTC
}

// Hub kontratları
var HubContracts = map[string]common.Address{
	"Main": common.HexToAddress("0x33381eC82DD811b1BABa841f1e2410468aeD7047"),
	"USDT": common.HexToAddress("0x3b0794015C9595aE06cf2069C0faC5d9B290f911"),
	"ETH":  common.HexToAddress("0x845A66F0230970971240d76fdDF7f961e08e3f01"),
	"WBTC": common.HexToAddress("0xec6595E48933D6f752a6f6421f0a9A019Fb80081"),
}

// BalanceResponse balance sorgusu için response
type BalanceResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token"`
	Address string `json:"address"`
	Balance string `json:"balance"`
	Symbol  string `json:"symbol"`
	Error   string `json:"error,omitempty"`
}

// getRPCURL: ARBITRUM_RPC yoksa alternatif env'leri dener; Ankr key ekler
func getRPCURL() (string, error) {
	candidate := func(v string) string {
		s := strings.TrimSpace(v)
		if s == "" {
			return ""
		}
		// Satır başı '#' veya tümüyle yorum gibi görünen değerleri yok say
		if strings.HasPrefix(s, "#") {
			return ""
		}
		return s
	}

	rpcUrl := candidate(os.Getenv("ARBITRUM_RPC"))
	if rpcUrl == "" {
		rpcUrl = candidate(os.Getenv("ARBITRUM_HTTP_RPC"))
	}
	if rpcUrl == "" {
		rpcUrl = candidate(os.Getenv("RPC_HTTP"))
	}
	if rpcUrl == "" {
		rpcUrl = candidate(os.Getenv("ANKR_HTTP"))
	}
	if rpcUrl == "" {
		return "", fmt.Errorf("RPC URL not set (ARBITRUM_RPC/ARBITRUM_HTTP_RPC/RPC_HTTP/ANKR_HTTP)")
	}
	low := strings.ToLower(rpcUrl)
	if strings.Contains(low, "rpc.ankr.com/arbitrum") && !strings.Contains(low, "/") {
		if key := candidate(os.Getenv("ANKR_API_KEY")); key != "" {
			if !strings.HasSuffix(rpcUrl, "/"+key) {
				rpcUrl = strings.TrimRight(rpcUrl, "/") + "/" + key
			}
		}
	}
	return rpcUrl, nil
}

// GetTokenBalance belirtilen token'ın balance'ını döner
func GetTokenBalance(token string) (*BalanceResponse, error) {
	rpcUrl, err := getRPCURL()
	if err != nil {
		return nil, err
	}

	client, err := ethclient.Dial(rpcUrl)
	if err != nil {
		return nil, fmt.Errorf("RPC bağlantısı kurulamadı: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Token adını büyük harfe çevir
	tokenUpper := strings.ToUpper(token)

	// Hub kontratını bul
	hubAddr, exists := HubContracts[tokenUpper]
	if !exists {
		return &BalanceResponse{
			Success: false,
			Token:   token,
			Error:   fmt.Sprintf("Desteklenmeyen token: %s", token),
		}, nil
	}

	// Token kontratını bul
	tokenAddr, exists := TokenContracts[tokenUpper]
	if !exists {
		return &BalanceResponse{
			Success: false,
			Token:   token,
			Error:   fmt.Sprintf("Token kontratı bulunamadı: %s", token),
		}, nil
	}

	// ERC20 balanceOf çağrısı
	data := []byte{0x70, 0xa0, 0x82, 0x31}   // balanceOf(address) function selector
	data = append(data, make([]byte, 12)...) // padding
	data = append(data, hubAddr.Bytes()...)

	msg := ethereum.CallMsg{
		To:   &tokenAddr,
		Data: data,
	}

	result, err := client.CallContract(ctx, msg, nil)
	if err != nil {
		return &BalanceResponse{
			Success: false,
			Token:   token,
			Error:   fmt.Sprintf("Kontrat çağrısı hatası: %v", err),
		}, nil
	}

	if len(result) < 32 {
		return &BalanceResponse{
			Success: false,
			Token:   token,
			Error:   "Geçersiz response uzunluğu",
		}, nil
	}

	balance := new(big.Int).SetBytes(result)

	// Decimal'ları ayarla
	var decimals int
	switch tokenUpper {
	case "USDT":
		decimals = 6
	case "ETH":
		decimals = 18
	case "WBTC":
		decimals = 8
	default:
		decimals = 18
	}

	// Balance'ı formatla
	balanceStr := formatBalance(balance, decimals)

	return &BalanceResponse{
		Success: true,
		Token:   tokenUpper,
		Address: hubAddr.Hex(),
		Balance: balanceStr,
		Symbol:  tokenUpper,
	}, nil
}

// GetMainBalance ana kontratın ETH balance'ını döner
func GetMainBalance() (*BalanceResponse, error) {
	rpcUrl, err := getRPCURL()
	if err != nil {
		return nil, err
	}

	client, err := ethclient.Dial(rpcUrl)
	if err != nil {
		return nil, fmt.Errorf("RPC bağlantısı kurulamadı: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	mainAddr := HubContracts["Main"]

	balance, err := client.BalanceAt(ctx, mainAddr, nil)
	if err != nil {
		return &BalanceResponse{
			Success: false,
			Token:   "Main",
			Error:   fmt.Sprintf("Balance sorgusu hatası: %v", err),
		}, nil
	}

	balanceStr := formatBalance(balance, 18)

	return &BalanceResponse{
		Success: true,
		Token:   "Main",
		Address: mainAddr.Hex(),
		Balance: balanceStr,
		Symbol:  "ETH",
	}, nil
}

// GetMainTokenBalance ana kontratın belirtilen token balance'ını döner
func GetMainTokenBalance(token string) (*BalanceResponse, error) {
	rpcUrl, err := getRPCURL()
	if err != nil {
		return nil, err
	}

	client, err := ethclient.Dial(rpcUrl)
	if err != nil {
		return nil, fmt.Errorf("RPC bağlantısı kurulamadı: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Token adını büyük harfe çevir
	tokenUpper := strings.ToUpper(token)

	// Token kontratını bul
	tokenAddr, exists := TokenContracts[tokenUpper]
	if !exists {
		return &BalanceResponse{
			Success: false,
			Token:   token,
			Error:   fmt.Sprintf("Token kontratı bulunamadı: %s", token),
		}, nil
	}

	// Main kontrat adresi
	mainAddr := HubContracts["Main"]

	// ERC20 balanceOf çağrısı
	data := []byte{0x70, 0xa0, 0x82, 0x31}   // balanceOf(address) function selector
	data = append(data, make([]byte, 12)...) // padding
	data = append(data, mainAddr.Bytes()...)

	msg := ethereum.CallMsg{
		To:   &tokenAddr,
		Data: data,
	}

	result, err := client.CallContract(ctx, msg, nil)
	if err != nil {
		return &BalanceResponse{
			Success: false,
			Token:   token,
			Error:   fmt.Sprintf("Kontrat çağrısı hatası: %v", err),
		}, nil
	}

	if len(result) < 32 {
		return &BalanceResponse{
			Success: false,
			Token:   token,
			Error:   "Geçersiz response uzunluğu",
		}, nil
	}

	balance := new(big.Int).SetBytes(result)

	// Decimal'ları ayarla
	var decimals int
	switch tokenUpper {
	case "USDT":
		decimals = 6
	case "ETH":
		decimals = 18
	case "WBTC":
		decimals = 8
	default:
		decimals = 18
	}

	// Balance'ı formatla
	balanceStr := formatBalance(balance, decimals)

	return &BalanceResponse{
		Success: true,
		Token:   "Main_" + tokenUpper,
		Address: mainAddr.Hex(),
		Balance: balanceStr,
		Symbol:  tokenUpper,
	}, nil
}

// formatBalance balance'ı okunabilir formata çevirir
func formatBalance(balance *big.Int, decimals int) string {
	if balance.Cmp(big.NewInt(0)) == 0 {
		return "0"
	}

	// Decimal'ları ayarla
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)

	// Tam sayı kısmı
	whole := new(big.Int).Div(balance, divisor)

	// Ondalık kısım
	remainder := new(big.Int).Mod(balance, divisor)

	if remainder.Cmp(big.NewInt(0)) == 0 {
		return whole.String()
	}

	// Ondalık kısmı string'e çevir ve padding ekle
	remainderStr := remainder.String()
	for len(remainderStr) < decimals {
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
