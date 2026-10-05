// Package mockpay 本地模拟身份核验与支付：不依赖任何外部服务，
// 用确定性交易号表达「核验通过 / 支付成功」，便于演示与对账。
package mockpay

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func digest(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte("|"))
		}
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))[:10]
}

// VerifyIdentity 模拟刷脸/刷卡身份核验，返回模拟核验流水号。
func VerifyIdentity(seniorID uint, cardNo, mealDate, mealType string) (bool, string) {
	tx := "V-" + digest(fmt.Sprint(seniorID), cardNo, mealDate, mealType)
	return true, tx
}

// Pay 模拟支付（老人自付部分），返回模拟支付流水号。
func Pay(seniorID uint, amountFen int64, mealDate, mealType string) string {
	return "P-" + digest(fmt.Sprint(seniorID), fmt.Sprint(amountFen), mealDate, mealType)
}

// SettleAdjustment 模拟补差款拨付/追回。
func SettleAdjustment(voucherID uint, deltaFen int64) string {
	return fmt.Sprintf("ADJ-%d-%s", voucherID, digest(fmt.Sprint(voucherID), fmt.Sprint(deltaFen)))
}
