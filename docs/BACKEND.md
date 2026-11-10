# Backend Reference

This document describes the backend as implemented in this repository. It covers the Go/Gin API and PostgreSQL persistence only.

## Components

| Layer | Location | Responsibility |
|---|---|---|
| HTTP handler | `internal/handler` | Binds and validates JSON, calls the service, and maps results to HTTP responses. |
| Service | `internal/service` | Validates transfer requests and orchestrates idempotency, wallet locking, balance updates, ledger writes, and state changes in a repository transaction. |
| Repository interface | `internal/repository` | Defines transaction and persistence operations used by the service. |
| PostgreSQL repository | `internal/repository/postgres` | Applies the schema and implements the transaction operations using pgx. |
| Domain | `internal/domain` | Defines transfer request/result types, statuses, validation, and permitted transitions. |
| Server entry point | `cmd/server/main.go` | Loads configuration, connects to PostgreSQL, applies the schema, and starts Gin on port `8080`. |

## HTTP API

### `POST /transfers`

Creates a wallet-to-wallet transfer.

| Item | Value |
|---|---|
| Headers | Send `Content-Type: application/json`. |
| Path parameters | None. |
| Query parameters | None. |

Request body:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

The request uses string wallet IDs and a positive integer amount. The IDs must differ. Both wallet rows must already exist; the backend does not provide a wallet-creation endpoint.

#### Responses

| HTTP status | Meaning | Body |
|---|---|---|
| `201 Created` | Transfer processed. | Transfer result JSON with `transferId`, request fields, and `status: "PROCESSED"`. |
| `200 OK` | Transfer attempt was recorded as failed (for example, insufficient funds). | Transfer result JSON with `status: "FAILED"` and `failureReason`. |
| `400 Bad Request` | Invalid JSON, required field missing, nonpositive amount, same source and destination, or blank required ID/key. | `{"error":"invalid request"}` for JSON binding errors or `{"error":"invalid transfer"}` for domain validation errors. |
| `404 Not Found` | Source or destination wallet does not exist. | `{"error":"wallet not found"}` |
| `500 Internal Server Error` | Other transfer processing error. | `{"error":"transfer could not be processed"}` |

Example processed response:

```json
{
  "transferId": 1,
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "status": "PROCESSED"
}
```

The response format and status codes are implementation choices; the assignment does not specify them.

## Persistence model

The embedded schema in [`internal/repository/postgres/001_init.sql`](../internal/repository/postgres/001_init.sql) creates:

- `wallets`: wallet ID, stored nonnegative `BIGINT` balance, and creation time.
- `transfers`: transfer ID, globally unique idempotency key, source and destination wallet foreign keys, positive `BIGINT` amount, state, optional failure reason, and creation time.
- `ledger_entries`: wallet and transfer foreign keys, `DEBIT` or `CREDIT` type, positive amount, and creation time. A unique `(transfer_id, type)` constraint allows at most one entry of each type per transfer.

Indexes are created for ledger lookup by wallet and transfer lookup by source or destination wallet. The idempotency key is stored on `transfers`; a separate idempotency table is not used.

## Transfer transaction

The service and repository execute each new transfer in one PostgreSQL transaction:

1. Return a previously stored result when the idempotency key already exists.
2. Insert a `PENDING` transfer row. The unique key also handles requests racing on the same key; the losing request reads and returns the committed original result.
3. Lock the source and destination wallet rows in sorted wallet-ID order.
4. If the source balance is insufficient, set the transfer to `FAILED` with reason `insufficient funds`, then commit without changing balances or writing ledger entries.
5. Otherwise debit the source, credit the destination, insert one `DEBIT` and one `CREDIT` ledger entry for the same amount, and set the transfer to `PROCESSED`.
6. Commit. Any persistence error rolls the transaction back, preventing partial balance or ledger changes.

The sorted row locks serialize concurrent balance changes and reduce deadlocks between transfers in opposite directions. A repeated key returns the original transfer outcome, including a stored `FAILED` outcome, without repeating side effects.

**Ledger behavior:** processed transfers have exactly two entries. Failed attempts have no ledger entries because they do not move funds. This is the implementation's interpretation of the assignment's ledger rule for failed transfers.

## Transfer states

Allowed values are `PENDING`, `PROCESSED`, and `FAILED`. The application permits `PENDING -> PROCESSED` and `PENDING -> FAILED`; `SetStatus` only accepts a terminal state while the stored row is `PENDING`. The new transfer row and its final status are committed in the same transaction, so a `PENDING` row is normally not visible outside the in-flight transaction.

## Configuration and running

The server reads `DATABASE_URL` from the process environment. It also reads `.env` from the current directory or a parent directory up to the Go module root. An environment variable already set in the shell takes precedence over the `.env` value.

Create the PostgreSQL database named in the connection URL before starting the server. The server applies the embedded schema at startup.

From the repository root:

```powershell
go run ./cmd/server
```

The API listens on `:8080`. Seed existing wallets through PostgreSQL before making transfer requests; wallet management is outside this backend API.

## Verification

Run the Go tests and static checks:

```powershell
go test ./...
go vet ./...
```

PostgreSQL integration tests use `TEST_DATABASE_URL`. They create a random isolated schema for each test and remove that schema during cleanup. The suite includes repository and Gin API integration coverage for success, idempotency, ledger entries, insufficient funds, validation, missing wallets, and concurrent debits.
