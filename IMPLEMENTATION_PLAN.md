# Wallet Transfer Service — Implementation Plan (LLM Reference)

## Assignment boundary

Implement the required wallet transfer service in the existing Go/Gin project. Use the assignment's single required endpoint, `POST /transfers`. PostgreSQL is preferred; SQLite is acceptable. Optional balance/history endpoints, metrics, and additional retry-safe workflows are not required. Retry and duplicate safety for the transfer endpoint itself are required.

## API contract reference

### Endpoint

| Item | Contract |
|---|---|
| Method | `POST` |
| Path | `/transfers` |
| Purpose | Create a wallet-to-wallet transfer. |
| Headers | No required headers are specified in the assignment. |
| Path parameters | None. |
| Query parameters | None specified. |
| Request media/body example | JSON, shown below. |
| Response | A repeated `idempotencyKey` must return the original result. Exact response body and HTTP status are not specified. |

### Request body

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

The field names and sample values above are from the assignment. It does not prescribe field types, amount precision/range, validation rules, response fields, status codes, or error format. Specify such details only when required to implement the project, and record them as implementation choices rather than assignment facts.

## Layer responsibilities

| Layer | Assignment-aligned responsibility |
|---|---|
| Gin handler | Decode and validate the HTTP request, map transport data, invoke service logic, and map the result. Keep it thin. |
| Service | Apply transfer business logic, orchestrate workflow, enforce idempotency behavior, and coordinate persistence. |
| Repository | Perform persistence and database interactions. |
| Domain | Hold entities and state transitions, and apply domain validation rules. |

## Step-by-step implementation

1. **Inspect the project.** Identify the Go module, current Gin setup, persistence dependencies, existing conventions, and any current API contract. Preserve project conventions where they fit the assignment.
2. **Record implementation choices.** This implementation uses PostgreSQL, stored balances, a database transaction, row locks in sorted wallet-ID order, and a unique idempotency key on each transfer. The assignment prefers PostgreSQL and lists database transactions, row-level locks, optimistic locking, and isolation levels as possible concurrency approaches.
3. **Define the domain.** Represent a transfer and its permitted states: `PENDING`, `PROCESSED`, `FAILED`. Permit the described transitions `PENDING -> PROCESSED` and `PENDING -> FAILED`; ensure retries and duplicates do not cause unsafe transitions.
4. **Define persistence.** Use the suggested `wallets`, `transfers`, `ledger_entries`, and `idempotency_records` tables as applicable. Add constraints needed for data integrity, unique idempotency detection, and ledger/transfer association. Keep the schema aligned to required behavior.
5. **Implement repository operations.** Provide the persistence operations the service needs. Ensure the selected transaction and locking/concurrency approach protects balance changes and ledger writes.
6. **Implement service workflow.** Detect a previously used idempotency key and return its original result without repeating side effects. For a new request, coordinate the transfer and persistence so execution is atomic. Apply the debit to the source and credit to the destination for the same amount, write exactly two corresponding ledger entries, and make the transfer state safe to retry. An insufficient-funds attempt is stored as `FAILED` without balance or ledger effects; an unknown wallet is returned as not found without a transfer row.
7. **Implement the Gin endpoint.** Register `POST /transfers`. Parse the request body, perform request validation, invoke the service, and map the result. Do not add endpoints absent from required scope.
8. **Add behavioral tests.** Cover transfer execution, idempotency, exactly two balanced ledger entries, failure without partial effects, and concurrent debits if possible. Use Red, Blue, Green: failing test, smallest correct implementation, safe refactor.
9. **Review the design against the assignment.** Verify atomicity, correct balances, duplicate safety, state transitions, concurrency safety, and the four handler/service/repository/domain responsibilities. Record the schema, idempotency strategy, concurrency handling, and assumptions/tradeoffs for the PR explanation.
10. **Prepare the required PR material.** Explain the database schema, idempotency strategy, concurrency handling, and assumptions or tradeoffs. Include an AI-use note naming the tool and describing generally how it was used, plus the full AI session transcript in the repository or by email; if that is not possible, provide all prompts used.

## Required invariants

- Every completed transfer has exactly two ledger entries.
- The source wallet entry is `DEBIT`; the destination wallet entry is `CREDIT`.
- Both entries refer to the same transfer and amount; the ledger balances.
- The transfer executes atomically.
- Reusing an idempotency key returns the original result and does not execute a duplicate transfer.
- Concurrent operations maintain correct balances, prevent double spending, and preserve consistent ledger entries.
- Transfer states remain within `PENDING`, `PROCESSED`, `FAILED` and follow the allowed transitions.

## Unspecified by the assignment

The assignment does not define exact response JSON, HTTP status codes, error format, required headers, query parameters, validation limits, amount representation, or a particular database schema/locking implementation. These must not be presented as requirements supplied by the assignment.

## Implementation-specific API mapping

The implementation returns the transfer JSON with HTTP `201` for `PROCESSED` and `200` for `FAILED`; it returns `400` for invalid request data, `404` for an unknown wallet, and `500` for other processing errors. Amounts are positive integer units. These are implementation choices because the assignment leaves them unspecified.
