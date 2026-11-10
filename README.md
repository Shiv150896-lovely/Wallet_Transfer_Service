# Wallet Transfer Service

Go/Gin wallet transfer API backed by PostgreSQL.

## Run

Set `DATABASE_URL` in `.env` to a PostgreSQL connection string and start the service. The server reads `.env` from the project root; values already set in the shell take precedence.

Create the database named in the connection string first. For the sample URL, create `wallets` in PostgreSQL:

```sql
CREATE DATABASE wallets;
```

```powershell
$env:DATABASE_URL = "postgres://user:password@localhost:5432/wallets?sslmode=disable"
go run ./cmd/server
```

The service applies its schema at startup. Wallets are stored in the `wallets` table; wallet creation is not part of the required API, so add wallets through the database before transferring:

```sql
INSERT INTO wallets (wallet_id, balance)
VALUES ('wallet_1', 1000), ('wallet_2', 0);
```

## Create a transfer

`POST http://localhost:8080/transfers`

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

Amounts are represented as positive integer units. The successful response contains the transfer ID, request fields, and status. Reusing an idempotency key returns the original transfer result.

## Verify

```powershell
go test ./...
```

PostgreSQL integration tests run when `TEST_DATABASE_URL` is set. They create and remove an isolated schema for each test.
