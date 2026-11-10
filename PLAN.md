# Wallet Transfer Service — Plan

## Objective

Build a small wallet-to-wallet transfer service in Go using Gin. The service must provide idempotent transfer requests, atomic transfers, correct balances, a balanced double-entry ledger, safe transfer states, and concurrency safety.

## Scope from the assignment

### Required

- `POST /transfers` to create a transfer.
- Idempotency using the request's `idempotencyKey`; a repeated key returns the original result and does not cause a second transfer.
- Atomic transfer execution.
- Correct wallet balance tracking, either derived from the ledger or stored and updated during transfer.
- Exactly two ledger entries per transfer: a debit from the source wallet and a credit to the destination wallet, for the transfer amount.
- Transfer states `PENDING`, `PROCESSED`, and `FAILED`, with the lifecycle `PENDING -> PROCESSED` or `PENDING -> FAILED`.
- Safe concurrent execution, including concurrent debits from the same wallet, with no double spending or inconsistent ledger entries.
- Persistence using PostgreSQL (preferred) or SQLite (acceptable).
- Layer separation: handler, service, repository, and domain models.
- Behavioral tests for transfer execution, idempotency, ledger correctness, and failures; concurrency safety tests if possible.
- Documentation-first workflow covering behavior, contract, side effects, failure and retry behavior, consistency, observability expectations, and testing strategy.

### Optional enhancements in assignment

Wallet balance API, transfer history API, observability/logging, metrics, and retry-safe workflows as an enhancement. Retry and duplicate safety for the required transfer endpoint remain required by the core requirements.

## API map

| Method and endpoint | Purpose | Input | Output requirement |
|---|---|---|---|
| `POST /transfers` | Create and execute a wallet transfer idempotently. | JSON body fields below. | Return the original result for a repeated `idempotencyKey`. The assignment does not define an exact response schema or HTTP status. |

### `POST /transfers` request contract

- Headers: the assignment specifies no required headers.
- Path parameters: none.
- Query parameters: none specified.
- JSON body:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

The assignment gives these field names and an example, but does not define field types beyond the example, validation limits, or a response body format. Do not treat unspecified details as assignment requirements.

## Selected implementation decisions

- Use PostgreSQL, preferred by the assignment.
- Store wallet balances and update them in the same transaction as the ledger and transfer state.
- Lock both wallet rows in sorted wallet-ID order inside the transaction to serialize concurrent balance changes and avoid opposing-transfer deadlocks.
- Store the unique idempotency key on the transfer row. A duplicate key returns the stored transfer outcome without repeating side effects.
- Keep insufficient-funds attempts as `FAILED` transfers with no balance or ledger changes. A successful `PROCESSED` transfer has exactly two ledger entries. An unknown wallet is returned as not found and does not create a transfer row.
- Represent amount as a positive integer number of units (`BIGINT` in PostgreSQL); the assignment's example uses an integer and does not specify fractional amounts.
- Implementation response mapping: `201` for a processed transfer, `200` for a stored failed transfer, `400` for invalid input, `404` for unknown wallets, and `500` for other processing errors. The assignment does not prescribe these codes or the response JSON.

## Implementation sequence

1. Inspect the existing Go/Gin project and current API contract.
2. Confirm the schema, balance approach, transaction and concurrency strategy, and idempotency storage behavior in this design.
3. Define domain types and permitted transfer state transitions.
4. Define repository operations and database schema for wallets, transfers, ledger entries, and idempotency records as needed.
5. Implement service orchestration so transfer state, balance effects, ledger entries, and idempotency behavior share the required atomic boundary.
6. Implement the thin Gin handler for `POST /transfers`: decode and validate the request, call the service, and map the result.
7. Add behavioral tests for successful transfers, duplicate requests, ledger entries, failures, and concurrency if possible.
8. Verify consistency and document the selected schema, idempotency strategy, concurrency handling, and assumptions/tradeoffs.

## Required behavior and consistency checks

- One accepted transfer produces exactly one source debit and one destination credit for the same amount.
- A repeated idempotency key returns the original result and produces no additional transfer effects.
- Transfer execution is atomic; partial balance or ledger effects must not persist.
- State transitions follow the allowed lifecycle and remain safe on retries and duplicates.
- Concurrent transfers preserve correct balances, prevent double spending, and keep ledger entries consistent.

## Failure and retry cases to cover

- Duplicate request with the same idempotency key.
- Transfer execution failure without partial effects.
- Retry or duplicate delivery while a transfer is being processed or after it has completed.
- Concurrent attempts to debit the same wallet.

The assignment does not enumerate specific validation or infrastructure error responses; define those only as implementation decisions.

## Testing strategy

Use behavioral tests for transfer execution, idempotency, ledger correctness, and failure scenarios. Include a concurrency test if possible. Follow the assignment's Red, Blue, Green discipline: write a failing behavior test, implement the smallest correct change, then refactor while keeping tests passing.

## Observability expectation

The documentation-first workflow asks that observability expectations be addressed. Logging and metrics are listed as optional enhancements, so this plan does not make them required features.

## PR submission notes required by the assignment

- Explain the database schema, idempotency strategy, concurrency handling, and assumptions or tradeoffs.
- Include an AI-use note naming the tool used and describing generally how it was used.
- Include the full AI session transcript in the repository or send it by email with the submission; if that is not possible, provide all prompts used.
