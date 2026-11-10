package service

import (
	"context"
	"testing"

	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/repository"
)

type recordingRepository struct {
	called  bool
	tx      *recordingTransaction
	balance int64
}

type recordingTransaction struct {
	transfer domain.Transfer
	updated  bool
	ledger   bool
	balance  int64
}

func (r *recordingRepository) WithinTransaction(_ context.Context, work func(repository.TransferTransaction) (domain.Transfer, error)) (domain.Transfer, error) {
	r.called = true
	balance := r.balance
	if balance == 0 {
		balance = 100
	}
	r.tx = &recordingTransaction{balance: balance}
	return work(r.tx)
}

func (t *recordingTransaction) FindByIdempotencyKey(context.Context, string) (domain.Transfer, bool, error) {
	return domain.Transfer{}, false, nil
}

func (t *recordingTransaction) CreatePending(_ context.Context, req domain.TransferRequest) (domain.Transfer, bool, error) {
	t.transfer = domain.Transfer{ID: 1, IdempotencyKey: req.IdempotencyKey, FromWalletID: req.FromWalletID, ToWalletID: req.ToWalletID, Amount: req.Amount, Status: domain.StatusPending}
	return t.transfer, true, nil
}

func (*recordingTransaction) LockWallets(context.Context, string, string) error { return nil }
func (t *recordingTransaction) Balance(context.Context, string) (int64, error)  { return t.balance, nil }
func (t *recordingTransaction) UpdateBalances(context.Context, string, string, int64) error {
	t.updated = true
	return nil
}
func (t *recordingTransaction) InsertLedgerPair(context.Context, int64, string, string, int64) error {
	t.ledger = true
	return nil
}
func (t *recordingTransaction) SetStatus(_ context.Context, _ int64, status domain.TransferStatus, reason string) error {
	t.transfer.Status = status
	t.transfer.FailureReason = reason
	return nil
}

func TestCreateTransferDelegatesValidRequest(t *testing.T) {
	repo := &recordingRepository{}
	svc := New(repo)
	req := domain.TransferRequest{IdempotencyKey: "key", FromWalletID: "one", ToWalletID: "two", Amount: 25}
	got, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !repo.called || got.Status != domain.StatusProcessed || got.Amount != req.Amount || !repo.tx.updated || !repo.tx.ledger {
		t.Fatalf("Create() = %+v, repository called = %v, tx=%+v", got, repo.called, repo.tx)
	}
}

func TestCreateTransferRejectsInvalidRequestBeforePersistence(t *testing.T) {
	repo := &recordingRepository{}
	svc := New(repo)
	_, err := svc.Create(context.Background(), domain.TransferRequest{IdempotencyKey: "key", FromWalletID: "one", ToWalletID: "two", Amount: -1})
	if err == nil {
		t.Fatal("Create() expected validation error")
	}
	if repo.called {
		t.Fatal("repository called for invalid request")
	}
}

func TestCreateTransferMarksInsufficientFundsFailed(t *testing.T) {
	repo := &recordingRepository{balance: 10}
	svc := New(repo)
	req := domain.TransferRequest{IdempotencyKey: "key", FromWalletID: "one", ToWalletID: "two", Amount: 25}
	got, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got.Status != domain.StatusFailed || got.FailureReason != domain.ErrInsufficient.Error() {
		t.Fatalf("Create() = %+v, want failed insufficient-funds transfer", got)
	}
	if repo.tx.updated || repo.tx.ledger {
		t.Fatal("insufficient-funds transfer must not change balances or write ledger entries")
	}
}
