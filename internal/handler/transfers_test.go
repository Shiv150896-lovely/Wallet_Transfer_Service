package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/repository"
	"wallet-transfer-service/internal/service"
)

type handlerRepository struct{}

type handlerTransaction struct{ transfer domain.Transfer }

func (handlerRepository) WithinTransaction(_ context.Context, work func(repository.TransferTransaction) (domain.Transfer, error)) (domain.Transfer, error) {
	return work(&handlerTransaction{})
}

func (*handlerTransaction) FindByIdempotencyKey(context.Context, string) (domain.Transfer, bool, error) {
	return domain.Transfer{}, false, nil
}

func (t *handlerTransaction) CreatePending(_ context.Context, req domain.TransferRequest) (domain.Transfer, bool, error) {
	t.transfer = domain.Transfer{ID: 1, IdempotencyKey: req.IdempotencyKey, FromWalletID: req.FromWalletID, ToWalletID: req.ToWalletID, Amount: req.Amount, Status: domain.StatusPending}
	return t.transfer, true, nil
}

func (*handlerTransaction) LockWallets(context.Context, string, string) error           { return nil }
func (*handlerTransaction) Balance(context.Context, string) (int64, error)              { return 100, nil }
func (*handlerTransaction) UpdateBalances(context.Context, string, string, int64) error { return nil }
func (*handlerTransaction) InsertLedgerPair(context.Context, int64, string, string, int64) error {
	return nil
}
func (t *handlerTransaction) SetStatus(_ context.Context, _ int64, status domain.TransferStatus, reason string) error {
	t.transfer.Status = status
	t.transfer.FailureReason = reason
	return nil
}

func TestCreateTransferEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, New(service.New(handlerRepository{})))
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(`{"idempotencyKey":"key","fromWalletId":"one","toWalletId":"two","amount":25}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"status":"PROCESSED"`) || !strings.Contains(response.Body.String(), `"amount":25`) {
		t.Fatalf("unexpected response body: %s", response.Body.String())
	}
}

func TestCreateTransferEndpointRejectsMalformedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, New(service.New(handlerRepository{})))
	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(`{"idempotencyKey":"key"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
