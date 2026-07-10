package webrpc

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// railSettledTopic is keccak256("RailSettled(uint256,uint256,uint256,uint256,uint256,uint256)").
const railSettledTopic = "0x14e2efd598f2db6bfe762fcf9a830ffdfcba170d263d4a4956f36176ba82d3f3"

type receiptGasFields struct {
	GasUsed           string `db:"gas_used"`
	EffectiveGasPrice string `db:"gas_price"`
}

// ethLogDataHexSQL strips only a leading 0x/0X prefix from log.value.
// Do NOT use ltrim(..., '0x'): that removes any leading 0/x characters.
const ethLogDataHexSQL = `lower(CASE
	WHEN left(coalesce(log.value->>'data', ''), 2) IN ('0x', '0X')
		THEN substr(log.value->>'data', 3)
	ELSE coalesce(log.value->>'data', '')
END)`

// ethLogDataUint256Word1SQL extracts RailSettled.totalNetPayeeAmount
// (second non-indexed ABI word: hex chars 65..128) as numeric.
// Decoded as 4×uint64 via bit(64)::bigint — Yugabyte rejects bit(256)::numeric.
const ethLogDataUint256Word1SQL = `COALESCE((
	SELECT
		(CASE WHEN (('x' || substr(h, 65, 16))::bit(64))::bigint < 0
			THEN (('x' || substr(h, 65, 16))::bit(64))::bigint::numeric + power(2::numeric, 64)
			ELSE (('x' || substr(h, 65, 16))::bit(64))::bigint::numeric END) * power(2::numeric, 192) +
		(CASE WHEN (('x' || substr(h, 81, 16))::bit(64))::bigint < 0
			THEN (('x' || substr(h, 81, 16))::bit(64))::bigint::numeric + power(2::numeric, 64)
			ELSE (('x' || substr(h, 81, 16))::bit(64))::bigint::numeric END) * power(2::numeric, 128) +
		(CASE WHEN (('x' || substr(h, 97, 16))::bit(64))::bigint < 0
			THEN (('x' || substr(h, 97, 16))::bit(64))::bigint::numeric + power(2::numeric, 64)
			ELSE (('x' || substr(h, 97, 16))::bit(64))::bigint::numeric END) * power(2::numeric, 64) +
		(CASE WHEN (('x' || substr(h, 113, 16))::bit(64))::bigint < 0
			THEN (('x' || substr(h, 113, 16))::bit(64))::bigint::numeric + power(2::numeric, 64)
			ELSE (('x' || substr(h, 113, 16))::bit(64))::bigint::numeric END)
	FROM (
		SELECT CASE
			WHEN length(raw) % 2 = 1 THEN '0' || raw
			ELSE raw
		END AS h
		FROM (SELECT ` + ethLogDataHexSQL + ` AS raw) s
		WHERE length(raw) >= 128
		  AND raw ~ '^[0-9a-fA-F]+$'
	) norm
	WHERE length(h) >= 128
), 0)`

func parseSQLNumericInt(raw string) (*big.Int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return big.NewInt(0), nil
	}
	if i := strings.IndexByte(raw, '.'); i >= 0 {
		raw = raw[:i]
	}
	if raw == "" || raw == "0" {
		return big.NewInt(0), nil
	}
	out, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return nil, fmt.Errorf("invalid numeric integer %q", raw)
	}
	return out, nil
}

func parseJSONUint64(raw string) (uint64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	if strings.HasPrefix(raw, "0x") || strings.HasPrefix(raw, "0X") {
		return strconv.ParseUint(strings.TrimPrefix(strings.ToLower(raw), "0x"), 16, 64)
	}
	return strconv.ParseUint(raw, 10, 64)
}

func parseJSONBigInt(raw string) (*big.Int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return big.NewInt(0), nil
	}
	out := new(big.Int)
	if strings.HasPrefix(raw, "0x") || strings.HasPrefix(raw, "0X") {
		hex := strings.TrimPrefix(strings.ToLower(raw), "0x")
		if _, ok := out.SetString(hex, 16); !ok {
			return nil, fmt.Errorf("invalid hex bigint %q", raw)
		}
		return out, nil
	}
	if _, ok := out.SetString(raw, 10); !ok {
		return nil, fmt.Errorf("invalid decimal bigint %q", raw)
	}
	return out, nil
}

func receiptGasFeeFromFields(gasUsedStr, gasPriceStr string) (*big.Int, error) {
	gasUsed, err := parseJSONUint64(gasUsedStr)
	if err != nil {
		return nil, err
	}
	if gasUsed == 0 {
		return big.NewInt(0), nil
	}
	price, err := parseJSONBigInt(gasPriceStr)
	if err != nil {
		return nil, err
	}
	return new(big.Int).Mul(price, new(big.Int).SetUint64(gasUsed)), nil
}

type receiptLogJSON struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

func ethLogFromJSON(raw []byte) (ethtypes.Log, error) {
	var j receiptLogJSON
	if err := json.Unmarshal(raw, &j); err != nil {
		return ethtypes.Log{}, err
	}
	log := ethtypes.Log{
		Address: common.HexToAddress(j.Address),
		Data:    common.FromHex(j.Data),
	}
	for _, topic := range j.Topics {
		log.Topics = append(log.Topics, common.HexToHash(topic))
	}
	return log, nil
}

// stripEthHexPrefix removes only a leading 0x/0X.
func stripEthHexPrefix(data string) string {
	if len(data) >= 2 && (data[:2] == "0x" || data[:2] == "0X") {
		return data[2:]
	}
	return data
}

// ethLogDataUint256Word1 extracts the second ABI word from event data hex.
func ethLogDataUint256Word1(data string) (*big.Int, error) {
	h := strings.ToLower(stripEthHexPrefix(data))
	if len(h)%2 == 1 {
		h = "0" + h
	}
	if len(h) < 128 {
		return big.NewInt(0), nil
	}
	word := h[64:128]
	out := new(big.Int)
	if _, ok := out.SetString(word, 16); !ok {
		return nil, fmt.Errorf("invalid ABI word hex %q", word)
	}
	return out, nil
}
