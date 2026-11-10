package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/handler"
	"wallet-transfer-service/internal/service"
)

func transferAPI(repo *Repository) http.Handler {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler.RegisterRoutes(router, handler.New(service.New(repo)))
	return router
}

func sendTransferRequest(t *testing.T, api http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, req)
	return response
}

func TestTransferAPIEndToEnd(t *testing.T) {
	repo, pool := testRepository(t)
	seedWallet(t, pool, "source", 100)
	seedWallet(t, pool, "destination", 0)
	api := transferAPI(repo)
	body := `{"idempotencyKey":"api-key","fromWalletId":"source","toWalletId":"destination","amount":40}`

	first := sendTransferRequest(t, api, body)
	if first.Code != http.StatusCreated {
		t.Fatalf("successful transfer status = %d, body=%s", first.Code, first.Body.String())
	}
	var original domain.Transfer
	if err := json.Unmarshal(first.Body.Bytes(), &original); err != nil {
		t.Fatalf("decode successful response: %v", err)
	}
	if original.Status != domain.StatusProcessed || original.Amount != 40 {
		t.Fatalf("unexpected successful response: %+v", original)
	}

	// The existing key returns the stored result even if the replay body differs.
	replay := sendTransferRequest(t, api, `{"idempotencyKey":"api-key","fromWalletId":"missing","toWalletId":"missing-too","amount":90}`)
	if replay.Code != http.StatusCreated {
		t.Fatalf("replay status = %d, body=%s", replay.Code, replay.Body.String())
	}
	var replayed domain.Transfer
	if err := json.Unmarshal(replay.Body.Bytes(), &replayed); err != nil {
		t.Fatalf("decode replay response: %v", err)
	}
	if replayed != original {
		t.Fatalf("replay result %+v differs from original %+v", replayed, original)
	}

	var source, destination, entries int64
	if err := pool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE wallet_id='source'`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE wallet_id='destination'`).Scan(&destination); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM ledger_entries WHERE transfer_id=$1`, original.ID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if source != 60 || destination != 40 || entries != 2 {
		t.Fatalf("duplicate side effects detected: source=%d destination=%d ledger entries=%d", source, destination, entries)
	}

	failed := sendTransferRequest(t, api, `{"idempotencyKey":"failed-key","fromWalletId":"source","toWalletId":"destination","amount":1000}`)
	if failed.Code != http.StatusOK {
		t.Fatalf("insufficient-funds status = %d, body=%s", failed.Code, failed.Body.String())
	}
	var failedTransfer domain.Transfer
	if err := json.Unmarshal(failed.Body.Bytes(), &failedTransfer); err != nil {
		t.Fatalf("decode failed response: %v", err)
	}
	if failedTransfer.Status != domain.StatusFailed || failedTransfer.FailureReason != domain.ErrInsufficient.Error() {
		t.Fatalf("unexpected insufficient-funds response: %+v", failedTransfer)
	}
	failedReplay := sendTransferRequest(t, api, `{"idempotencyKey":"failed-key","fromWalletId":"source","toWalletId":"destination","amount":1}`)
	if failedReplay.Code != http.StatusOK || failedReplay.Body.String() != failed.Body.String() {
		t.Fatalf("failed-result replay differs: status=%d body=%s", failedReplay.Code, failedReplay.Body.String())
	}
	invalid := sendTransferRequest(t, api, `{"idempotencyKey":"bad","fromWalletId":"source","toWalletId":"destination","amount":0}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid request status = %d, want %d", invalid.Code, http.StatusBadRequest)
	}
	missingWallet := sendTransferRequest(t, api, `{"idempotencyKey":"missing-wallet","fromWalletId":"missing","toWalletId":"destination","amount":1}`)
	if missingWallet.Code != http.StatusNotFound {
		t.Fatalf("missing wallet status = %d, want %d; body=%s", missingWallet.Code, http.StatusNotFound, missingWallet.Body.String())
	}
}

func TestTransferAPIConcurrentDebits(t *testing.T) {
	repo, pool := testRepository(t)
	seedWallet(t, pool, "source", 100)
	seedWallet(t, pool, "destination-one", 0)
	seedWallet(t, pool, "destination-two", 0)
	api := transferAPI(repo)

	bodies := []string{
		`{"idempotencyKey":"api-concurrent-one","fromWalletId":"source","toWalletId":"destination-one","amount":80}`,
		`{"idempotencyKey":"api-concurrent-two","fromWalletId":"source","toWalletId":"destination-two","amount":80}`,
	}
	responses := make([]*httptest.ResponseRecorder, len(bodies))
	var wg sync.WaitGroup
	for i, body := range bodies {
		wg.Add(1)
		go func(i int, body string) {
			defer wg.Done()
			responses[i] = sendTransferRequest(t, api, body)
		}(i, body)
	}
	wg.Wait()
	processed, failed := 0, 0
	for _, response := range responses {
		var transfer domain.Transfer
		if err := json.Unmarshal(response.Body.Bytes(), &transfer); err != nil {
			t.Fatalf("decode concurrent response: %v", err)
		}
		switch transfer.Status {
		case domain.StatusProcessed:
			processed++
			if response.Code != http.StatusCreated {
				t.Fatalf("processed status code = %d", response.Code)
			}
		case domain.StatusFailed:
			failed++
			if response.Code != http.StatusOK {
				t.Fatalf("failed status code = %d", response.Code)
			}
		default:
			t.Fatalf("unexpected concurrent response: code=%d transfer=%+v", response.Code, transfer)
		}
	}
	var source, destOne, destTwo, ledgerEntries int64
	for walletID, destination := range map[string]*int64{"source": &source, "destination-one": &destOne, "destination-two": &destTwo} {
		if err := pool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE wallet_id=$1`, walletID).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM ledger_entries`).Scan(&ledgerEntries); err != nil {
		t.Fatal(err)
	}
	if processed != 1 || failed != 1 || source != 20 || destOne+destTwo != 80 || ledgerEntries != 2 {
		t.Fatalf("processed=%d failed=%d balances=(%d,%d,%d) ledger entries=%d", processed, failed, source, destOne, destTwo, ledgerEntries)
	}
}
