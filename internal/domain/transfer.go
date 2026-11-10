package domain

import (
	"errors"
	"strings"
)

var (
	ErrInvalidTransfer = errors.New("invalid transfer")
	ErrNotFound        = errors.New("wallet not found")
	ErrInsufficient    = errors.New("insufficient funds")
)

type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey" binding:"required"`
	FromWalletID   string `json:"fromWalletId" binding:"required"`
	ToWalletID     string `json:"toWalletId" binding:"required"`
	Amount         int64  `json:"amount" binding:"required,gt=0"`
}

type Transfer struct {
	ID             int64          `json:"transferId"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"`
	Status         TransferStatus `json:"status"`
	FailureReason  string         `json:"failureReason,omitempty"`
}

func (r TransferRequest) Validate() error {
	if strings.TrimSpace(r.IdempotencyKey) == "" || strings.TrimSpace(r.FromWalletID) == "" || strings.TrimSpace(r.ToWalletID) == "" || r.Amount <= 0 || r.FromWalletID == r.ToWalletID {
		return ErrInvalidTransfer
	}
	return nil
}

func CanTransition(from, to TransferStatus) bool {
	return from == StatusPending && (to == StatusProcessed || to == StatusFailed)
}
