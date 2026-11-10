package repository

import (
	"context"

	"wallet-transfer-service/internal/domain"
)

type TransferRepository interface {
	WithinTransaction(context.Context, func(TransferTransaction) (domain.Transfer, error)) (domain.Transfer, error)
}

type TransferTransaction interface {
	FindByIdempotencyKey(context.Context, string) (domain.Transfer, bool, error)
	CreatePending(context.Context, domain.TransferRequest) (domain.Transfer, bool, error)
	LockWallets(context.Context, string, string) error
	Balance(context.Context, string) (int64, error)
	UpdateBalances(context.Context, string, string, int64) error
	InsertLedgerPair(context.Context, int64, string, string, int64) error
	SetStatus(context.Context, int64, domain.TransferStatus, string) error
}
