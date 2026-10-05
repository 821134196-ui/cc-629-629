package main

import (
	"fmt"
	"sync/atomic"
	"time"
)

// 本地模拟：身份核验服务与支付网关（均不依赖外部系统）
var txnSeq uint64

func mockTxnID(prefix string) string {
	n := atomic.AddUint64(&txnSeq, 1)
	return fmt.Sprintf("%s-%s-%06d", prefix, time.Now().Format("0102150405"), n)
}

func mockVerifyTxn() string  { return mockTxnID("VRF") }
func mockPayTxn(prefix, period string, elderlyID uint) string {
	if prefix == "ADJ" {
		n := atomic.AddUint64(&txnSeq, 1)
		return fmt.Sprintf("ADJ-%s-U%d-%06d", period, elderlyID, n)
	}
	return mockTxnID("PAY")
}

type verifyResult struct {
	OK      bool   `json:"ok"`
	TxnID   string `json:"txn_id"`
	Message string `json:"message"`
}

type payResult struct {
	OK      bool   `json:"ok"`
	TxnID   string `json:"txn_id"`
	Message string `json:"message"`
}

// verifyIdentity 本地模拟身份核验：卡号存在即核验通过
func verifyIdentity(e *Elderly) verifyResult {
	return verifyResult{OK: true, TxnID: mockVerifyTxn(),
		Message: fmt.Sprintf("本地身份核验通过：%s（卡号 %s）", e.Name, e.CardNo)}
}

// chargePayment 本地模拟支付：向老人收取自付部分
func chargePayment(amount float64) payResult {
	return payResult{OK: true, TxnID: mockPayTxn("PAY", "", 0),
		Message: fmt.Sprintf("本地模拟支付成功：自付 %.2f 元", amount)}
}
