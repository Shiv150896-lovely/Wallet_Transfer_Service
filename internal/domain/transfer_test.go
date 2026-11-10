package domain

import "testing"

func TestTransferRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		request TransferRequest
		wantErr bool
	}{
		{"valid", TransferRequest{IdempotencyKey: "key", FromWalletID: "one", ToWalletID: "two", Amount: 10}, false},
		{"missing key", TransferRequest{FromWalletID: "one", ToWalletID: "two", Amount: 10}, true},
		{"same wallet", TransferRequest{IdempotencyKey: "key", FromWalletID: "one", ToWalletID: "one", Amount: 10}, true},
		{"nonpositive amount", TransferRequest{IdempotencyKey: "key", FromWalletID: "one", ToWalletID: "two", Amount: 0}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.request.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestTransferStateTransitions(t *testing.T) {
	if !CanTransition(StatusPending, StatusProcessed) || !CanTransition(StatusPending, StatusFailed) {
		t.Fatal("expected PENDING to transition to PROCESSED or FAILED")
	}
	if CanTransition(StatusProcessed, StatusFailed) || CanTransition(StatusFailed, StatusProcessed) {
		t.Fatal("terminal transfer state must not transition")
	}
}
