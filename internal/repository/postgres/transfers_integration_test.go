package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/service"
)

func testRepository(t *testing.T) (*Repository, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set; PostgreSQL integration test skipped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(admin.Close)

	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "wallet_test_" + hex.EncodeToString(random[:])
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create isolated test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Errorf("drop isolated test schema: %v", err)
		}
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open isolated test connection pool: %v", err)
	}
	t.Cleanup(pool.Close)
	repo := New(pool)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatalf("apply test schema: %v", err)
	}
	return repo, pool
}

func seedWallet(t *testing.T, pool *pgxpool.Pool, id string, balance int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO wallets (wallet_id, balance) VALUES ($1, $2)`, id, balance); err != nil {
		t.Fatalf("seed wallet %s: %v", id, err)
	}
}

func TestCreateTransferAndIdempotentReplay(t *testing.T) {
	repo, pool := testRepository(t)
	seedWallet(t, pool, "source", 100)
	seedWallet(t, pool, "destination", 0)

	ctx := context.Background()
	req := domain.TransferRequest{IdempotencyKey: "same-key", FromWalletID: "source", ToWalletID: "destination", Amount: 40}
	first, err := service.New(repo).Create(ctx, req)
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}
	replay, err := service.New(repo).Create(ctx, domain.TransferRequest{IdempotencyKey: "same-key", FromWalletID: "missing", ToWalletID: "also-missing", Amount: 90})
	if err != nil {
		t.Fatalf("replay transfer: %v", err)
	}
	if first != replay || first.Status != domain.StatusProcessed {
		t.Fatalf("first result %+v differs from replay %+v", first, replay)
	}

	var source, destination, entries int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE wallet_id = 'source'`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE wallet_id = 'destination'`).Scan(&destination); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE transfer_id = $1`, first.ID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if source != 60 || destination != 40 || entries != 2 {
		t.Fatalf("balances source=%d destination=%d, ledger entries=%d", source, destination, entries)
	}
	ledgerRows, err := pool.Query(ctx, `SELECT wallet_id, type, amount FROM ledger_entries WHERE transfer_id = $1`, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer ledgerRows.Close()
	gotLedger := map[string]struct {
		typeName string
		amount   int64
	}{}
	for ledgerRows.Next() {
		var walletID, typeName string
		var amount int64
		if err := ledgerRows.Scan(&walletID, &typeName, &amount); err != nil {
			t.Fatal(err)
		}
		gotLedger[walletID] = struct {
			typeName string
			amount   int64
		}{typeName: typeName, amount: amount}
	}
	if err := ledgerRows.Err(); err != nil {
		t.Fatal(err)
	}
	if gotLedger["source"].typeName != "DEBIT" || gotLedger["source"].amount != 40 || gotLedger["destination"].typeName != "CREDIT" || gotLedger["destination"].amount != 40 {
		t.Fatalf("unexpected ledger entries: %+v", gotLedger)
	}
}

func TestInsufficientFundsIsFailedAndIdempotent(t *testing.T) {
	repo, pool := testRepository(t)
	seedWallet(t, pool, "source", 10)
	seedWallet(t, pool, "destination", 0)
	req := domain.TransferRequest{IdempotencyKey: "insufficient-key", FromWalletID: "source", ToWalletID: "destination", Amount: 20}

	first, err := service.New(repo).Create(context.Background(), req)
	if err != nil {
		t.Fatalf("create failed transfer: %v", err)
	}
	replay, err := service.New(repo).Create(context.Background(), req)
	if err != nil {
		t.Fatalf("replay failed transfer: %v", err)
	}
	var source, destination, entries int64
	if err := pool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE wallet_id = 'source'`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE wallet_id = 'destination'`).Scan(&destination); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM ledger_entries WHERE transfer_id = $1`, first.ID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if first.Status != domain.StatusFailed || first != replay || source != 10 || destination != 0 || entries != 0 {
		t.Fatalf("first=%+v replay=%+v balances=(%d,%d) entries=%d", first, replay, source, destination, entries)
	}
}

func TestConcurrentDebitsDoNotOverspend(t *testing.T) {
	repo, pool := testRepository(t)
	seedWallet(t, pool, "source", 100)
	seedWallet(t, pool, "destination-one", 0)
	seedWallet(t, pool, "destination-two", 0)

	requests := []domain.TransferRequest{
		{IdempotencyKey: "concurrent-one", FromWalletID: "source", ToWalletID: "destination-one", Amount: 80},
		{IdempotencyKey: "concurrent-two", FromWalletID: "source", ToWalletID: "destination-two", Amount: 80},
	}
	results := make([]domain.Transfer, len(requests))
	errs := make([]error, len(requests))
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = service.New(repo).Create(context.Background(), requests[i])
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent transfer error: %v", err)
		}
	}
	processed, failed := 0, 0
	for _, transfer := range results {
		switch transfer.Status {
		case domain.StatusProcessed:
			processed++
		case domain.StatusFailed:
			failed++
		}
	}
	var source, destinationOne, destinationTwo, entries int64
	if err := pool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE wallet_id = 'source'`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE wallet_id = 'destination-one'`).Scan(&destinationOne); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE wallet_id = 'destination-two'`).Scan(&destinationTwo); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM ledger_entries`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if processed != 1 || failed != 1 || source != 20 || destinationOne+destinationTwo != 80 || entries != 2 {
		t.Fatalf("processed=%d failed=%d balances=(%d,%d,%d) ledger entries=%d", processed, failed, source, destinationOne, destinationTwo, entries)
	}
}
