package service

import (
	"context"
	"errors"

	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/repository"
)

type TransferService struct {
	repo repository.TransferRepository
}

func New(repo repository.TransferRepository) *TransferService {
	return &TransferService{repo: repo}
}

func (s *TransferService) Create(ctx context.Context, req domain.TransferRequest) (domain.Transfer, error) {
	if err := req.Validate(); err != nil {
		return domain.Transfer{}, err
	}
	return s.repo.WithinTransaction(ctx, func(tx repository.TransferTransaction) (domain.Transfer, error) {
		transfer, found, err := tx.FindByIdempotencyKey(ctx, req.IdempotencyKey)
		if err != nil || found {
			return transfer, err
		}

		transfer, created, err := tx.CreatePending(ctx, req)
		if err != nil || !created {
			return transfer, err
		}
		if err := tx.LockWallets(ctx, req.FromWalletID, req.ToWalletID); err != nil {
			return domain.Transfer{}, err
		}
		balance, err := tx.Balance(ctx, req.FromWalletID)
		if err != nil {
			return domain.Transfer{}, err
		}
		if balance < req.Amount {
			if err := transition(tx, ctx, &transfer, domain.StatusFailed, domain.ErrInsufficient.Error()); err != nil {
				return domain.Transfer{}, err
			}
			return transfer, nil
		}

		if err := tx.UpdateBalances(ctx, req.FromWalletID, req.ToWalletID, req.Amount); err != nil {
			return domain.Transfer{}, err
		}
		if err := tx.InsertLedgerPair(ctx, transfer.ID, req.FromWalletID, req.ToWalletID, req.Amount); err != nil {
			return domain.Transfer{}, err
		}
		if err := transition(tx, ctx, &transfer, domain.StatusProcessed, ""); err != nil {
			return domain.Transfer{}, err
		}
		return transfer, nil
	})
}

func transition(tx repository.TransferTransaction, ctx context.Context, transfer *domain.Transfer, next domain.TransferStatus, reason string) error {
	if !domain.CanTransition(transfer.Status, next) {
		return errors.New("invalid transfer state transition")
	}
	if err := tx.SetStatus(ctx, transfer.ID, next, reason); err != nil {
		return err
	}
	transfer.Status = next
	transfer.FailureReason = reason
	return nil
}
