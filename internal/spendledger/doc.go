// Package spendledger keeps a durable, append/update ledger of LLM spend
// entries keyed by session. Entries are stored in their own JSON file under
// the daemon data directory, independent of the session record, so deleting a
// session never removes its ledger entry. Entries older than the retention
// window are dropped by Prune; Horizon reports the oldest retained time so
// reports can tell "older than the ledger" from "no spend".
package spendledger
