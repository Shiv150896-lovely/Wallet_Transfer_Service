package postgres

import (
	"context"
	"embed"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/repository"
)

//go:embed 001_init.sql
var schemaFS embed.FS

type Repository struct {
	pool *pgxpool.Pool
}

type transaction struct {
	tx pgx.Tx
}

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Migrate(ctx context.Context) error {
	schema, err := schemaFS.ReadFile("001_init.sql")
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, string(schema))
	return err
}

func (r *Repository) WithinTransaction(ctx context.Context, work func(repository.TransferTransaction) (domain.Transfer, error)) (domain.Transfer, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Transfer{}, err
	}
	defer tx.Rollback(ctx)

	result, err := work(&transaction{tx: tx})
	if err != nil {
		return domain.Transfer{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Transfer{}, err
	}
	return result, nil
}

func (t *transaction) FindByIdempotencyKey(ctx context.Context, key string) (domain.Transfer, bool, error) {
	transfer, err := scanTransfer(t.tx.QueryRow(ctx, `
		SELECT transfer_id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, COALESCE(failure_reason, '')
		FROM transfers WHERE idempotency_key = $1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Transfer{}, false, nil
	}
	if err != nil {
		return domain.Transfer{}, false, err
	}
	return transfer, true, nil
}

func (t *transaction) CreatePending(ctx context.Context, req domain.TransferRequest) (domain.Transfer, bool, error) {
	transfer, err := scanTransfer(t.tx.QueryRow(ctx, `
		INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4, 'PENDING')
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING transfer_id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, COALESCE(failure_reason, '')`,
		req.IdempotencyKey, req.FromWalletID, req.ToWalletID, req.Amount))
	if errors.Is(err, pgx.ErrNoRows) {
		// A simultaneous request may have inserted the key after FindByIdempotencyKey.
		transfer, err = scanTransfer(t.tx.QueryRow(ctx, `
			SELECT transfer_id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, COALESCE(failure_reason, '')
			FROM transfers WHERE idempotency_key = $1`, req.IdempotencyKey))
		if err != nil {
			return domain.Transfer{}, false, err
		}
		return transfer, false, nil
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return domain.Transfer{}, false, domain.ErrNotFound
		}
		return domain.Transfer{}, false, err
	}
	return transfer, true, nil
}

func (t *transaction) LockWallets(ctx context.Context, fromWalletID, toWalletID string) error {
	walletIDs := []string{fromWalletID, toWalletID}
	if walletIDs[0] > walletIDs[1] {
		walletIDs[0], walletIDs[1] = walletIDs[1], walletIDs[0]
	}
	rows, err := t.tx.Query(ctx, `SELECT wallet_id FROM wallets WHERE wallet_id = ANY($1) ORDER BY wallet_id FOR UPDATE`, walletIDs)
	if err != nil {
		return err
	}
	locked := 0
	for rows.Next() {
		var walletID string
		if err := rows.Scan(&walletID); err != nil {
			rows.Close()
			return err
		}
		locked++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if locked != 2 {
		return domain.ErrNotFound
	}
	return nil
}

func (t *transaction) Balance(ctx context.Context, walletID string) (int64, error) {
	var balance int64
	err := t.tx.QueryRow(ctx, `SELECT balance FROM wallets WHERE wallet_id = $1`, walletID).Scan(&balance)
	return balance, err
}

func (t *transaction) UpdateBalances(ctx context.Context, fromWalletID, toWalletID string, amount int64) error {
	if _, err := t.tx.Exec(ctx, `UPDATE wallets SET balance = balance - $1 WHERE wallet_id = $2`, amount, fromWalletID); err != nil {
		return err
	}
	if _, err := t.tx.Exec(ctx, `UPDATE wallets SET balance = balance + $1 WHERE wallet_id = $2`, amount, toWalletID); err != nil {
		return err
	}
	return nil
}

func (t *transaction) InsertLedgerPair(ctx context.Context, transferID int64, fromWalletID, toWalletID string, amount int64) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount)
		VALUES ($1, $2, 'DEBIT', $3), ($4, $2, 'CREDIT', $3)`,
		fromWalletID, transferID, amount, toWalletID)
	return err
}

func (t *transaction) SetStatus(ctx context.Context, transferID int64, status domain.TransferStatus, reason string) error {
	if status != domain.StatusProcessed && status != domain.StatusFailed {
		return errors.New("invalid terminal transfer status")
	}
	tag, err := t.tx.Exec(ctx, `UPDATE transfers SET status = $1, failure_reason = NULLIF($2, '') WHERE transfer_id = $3 AND status = 'PENDING'`, status, reason, transferID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("transfer is not pending")
	}
	return nil
}

func scanTransfer(row pgx.Row) (domain.Transfer, error) {
	var transfer domain.Transfer
	err := row.Scan(&transfer.ID, &transfer.IdempotencyKey, &transfer.FromWalletID, &transfer.ToWalletID, &transfer.Amount, &transfer.Status, &transfer.FailureReason)
	return transfer, err
}
