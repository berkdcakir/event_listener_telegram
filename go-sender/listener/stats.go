package listener

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type DailyStat struct {
	Label          string `json:"label"`
	Address        string `json:"address"`
	Token          string `json:"token"`
	Symbol         string `json:"symbol"`
	Tx24h          int    `json:"tx24h"`
	Change24h      string `json:"change24h"`
	CurrentBalance string `json:"currentBalance"`
}

type DailyStatsResponse struct {
	FromBlock uint64      `json:"fromBlock"`
	ToBlock   uint64      `json:"toBlock"`
	FromTs    int64       `json:"fromTs"`
	ToTs      int64       `json:"toTs"`
	Items     []DailyStat `json:"items"`
}

func findBlockByTimestamp(ctx context.Context, client *ethclient.Client, target time.Time) (uint64, error) {
	head, err := client.BlockNumber(ctx)
	if err != nil {
		return 0, err
	}
	lo := uint64(0)
	hi := head
	for lo < hi {
		mid := (lo + hi) / 2
		hdr, err := client.HeaderByNumber(ctx, new(big.Int).SetUint64(mid))
		if err != nil {
			return 0, err
		}
		if hdr.Time >= uint64(target.Unix()) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo, nil
}

func erc20BalanceAt(ctx context.Context, client *ethclient.Client, token, holder common.Address, block *big.Int) (*big.Int, error) {
	// data = selector 0x70a08231 + 12 bytes pad + holder
	data := []byte{0x70, 0xa0, 0x82, 0x31}
	data = append(data, make([]byte, 12)...)
	data = append(data, holder.Bytes()...)
	msg := ethereum.CallMsg{To: &token, Data: data}
	b, err := client.CallContract(ctx, msg, block)
	if err != nil {
		return nil, err
	}
	if len(b) < 32 {
		return big.NewInt(0), nil
	}
	return new(big.Int).SetBytes(b), nil
}

func erc20TransfersCount(ctx context.Context, client *ethclient.Client, token, holder common.Address, fromBlock, toBlock *big.Int) (int, error) {
	transferTopic := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")
	// two queries: from=holder and to=holder
	count := 0
	qFrom := ethereum.FilterQuery{FromBlock: fromBlock, ToBlock: toBlock, Addresses: []common.Address{token}, Topics: [][]common.Hash{{transferTopic}, {common.BytesToHash(holder.Bytes())}}}
	logsFrom, err := client.FilterLogs(ctx, qFrom)
	if err == nil {
		count += len(logsFrom)
	}
	qTo := ethereum.FilterQuery{FromBlock: fromBlock, ToBlock: toBlock, Addresses: []common.Address{token}, Topics: [][]common.Hash{{transferTopic}, {}, {common.BytesToHash(holder.Bytes())}}}
	logsTo, err2 := client.FilterLogs(ctx, qTo)
	if err2 == nil {
		count += len(logsTo)
	}
	if err != nil && err2 != nil {
		return 0, err
	}
	return count, nil
}

func GetDailyStats(ctx context.Context, client *ethclient.Client) (*DailyStatsResponse, error) {
	nowHdr, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	nowTs := int64(nowHdr.Time)
	fromTs := time.Unix(nowTs, 0).Add(-24 * time.Hour)
	fromBlockNum, err := findBlockByTimestamp(ctx, client, fromTs)
	if err != nil {
		return nil, err
	}
	fromBlock := new(big.Int).SetUint64(fromBlockNum)
	toBlock := new(big.Int).SetUint64(nowHdr.Number.Uint64())

	items := []DailyStat{}

	// Define tracked pairs
	targets := []struct {
		label    string
		addr     common.Address
		token    common.Address
		symbol   string
		isNative bool
	}{
		{"USDT Hub", HubContracts["USDT"], TokenContracts["USDT"], "USDT", false},
		{"wETH Hub", HubContracts["ETH"], TokenContracts["ETH"], "ETH", false},
		{"wBTC Hub", HubContracts["WBTC"], TokenContracts["WBTC"], "WBTC", false},
		{"Main ETH", HubContracts["Main"], common.Address{}, "ETH", true},
		{"Main USDT", HubContracts["Main"], TokenContracts["USDT"], "USDT", false},
	}

	for _, t := range targets {
		var curr, prev *big.Int
		var txCount int
		if t.isNative {
			curr, err = client.BalanceAt(ctx, t.addr, toBlock)
			if err != nil {
				curr = big.NewInt(0)
			}
			prev, err = client.BalanceAt(ctx, t.addr, fromBlock)
			if err != nil {
				prev = big.NewInt(0)
			}
			txCount = 0 // Native transfer sayısını hızlıca saymak zor; 0 bırakıyoruz
		} else {
			curr, err = erc20BalanceAt(ctx, client, t.token, t.addr, toBlock)
			if err != nil {
				curr = big.NewInt(0)
			}
			prev, err = erc20BalanceAt(ctx, client, t.token, t.addr, fromBlock)
			if err != nil {
				prev = big.NewInt(0)
			}
			txCount, _ = erc20TransfersCount(ctx, client, t.token, t.addr, fromBlock, toBlock)
		}
		delta := new(big.Int).Sub(curr, prev)
		decimals := 18
		switch t.symbol {
		case "USDT":
			decimals = 6
		case "WBTC":
			decimals = 8
		case "ETH":
			decimals = 18
		}
		item := DailyStat{
			Label:          t.label,
			Address:        t.addr.Hex(),
			Token:          t.symbol,
			Symbol:         t.symbol,
			Tx24h:          txCount,
			Change24h:      formatBalance(delta, decimals),
			CurrentBalance: formatBalance(curr, decimals),
		}
		items = append(items, item)
	}

	return &DailyStatsResponse{
		FromBlock: fromBlock.Uint64(),
		ToBlock:   toBlock.Uint64(),
		FromTs:    fromTs.Unix(),
		ToTs:      nowTs,
		Items:     items,
	}, nil
}
