package store

import "context"

// Store persists and queries call records. Implementations must be
// safe for concurrent use. The audit log is treated as append-only:
// there is deliberately no Update or Delete method, since audit
// records must remain immutable for compliance purposes. Retention is
// handled by Prune, which is the only sanctioned way records leave the
// store, and every prune is expected to be driven by an explicit,
// configured retention policy rather than ad-hoc deletion.
type Store interface {
	// Append adds a new immutable call record.
	Append(ctx context.Context, rec CallRecord) error
	// Query returns records matching the filter, newest first.
	Query(ctx context.Context, f Filter) ([]CallRecord, error)
	// Stats aggregates records matching the filter.
	Stats(ctx context.Context, f Filter) (Stats, error)
	// Prune removes records older than the given retention filter and
	// returns the number of records removed.
	Prune(ctx context.Context, since Filter) (int, error)
}
