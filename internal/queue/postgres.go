package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/user/clone/internal/util"
)

// PostgresQueue is a PostgreSQL-backed queue storing items in the crawl_queue table.
type PostgresQueue struct {
	pool      *pgxpool.Pool
	key       string
	maxSize   int
	parentCtx context.Context
}

// NewPostgresQueue creates a PostgresQueue using the default max queue size.
func NewPostgresQueue(ctx context.Context, pgDSN string) (*PostgresQueue, error) {
	return NewPostgresQueueWithSize(ctx, pgDSN, DefaultMaxQueueSize)
}

// NewPostgresQueueWithSize connects to Postgres with the given maxSize and
// ensures the queue table and index exist.
func NewPostgresQueueWithSize(ctx context.Context, pgDSN string, maxSize int) (*PostgresQueue, error) {
	pool, err := pgxpool.New(ctx, pgDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}
	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS crawl_queue (
			url TEXT PRIMARY KEY,
			depth INTEGER NOT NULL DEFAULT 0,
			item_data JSONB NOT NULL,
			seen BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to create queue table: %w", err)
	}
	_, err = pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS idx_crawl_queue_depth ON crawl_queue(depth)
	`)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to create index: %w", err)
	}
	return &PostgresQueue{
		pool:      pool,
		key:       "crawl_queue",
		maxSize:   maxSize,
		parentCtx: ctx,
	}, nil
}

// PushURL inserts url at depth unless it already exists in the queue.
func (q *PostgresQueue) PushURL(url string, depth int) bool {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	item := URLItem{URL: url, Depth: depth}
	itemData, err := json.Marshal(item)
	if err != nil {
		return false
	}
	result, err := q.pool.Exec(opCtx,
		`INSERT INTO crawl_queue (url, depth, item_data, seen) VALUES ($1, $2, $3, TRUE)
		 ON CONFLICT (url) DO NOTHING`, url, depth, itemData)
	return err == nil && result.RowsAffected() > 0
}

// PopURL removes and returns the lowest-depth item from the queue.
func (q *PostgresQueue) PopURL() (URLItem, bool) {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	var item URLItem
	var itemData []byte
	err := q.pool.QueryRow(opCtx,
		`DELETE FROM crawl_queue WHERE ctid = (
			SELECT ctid FROM crawl_queue ORDER BY depth ASC LIMIT 1 FOR UPDATE SKIP LOCKED
		) RETURNING url, depth, item_data`,
	).Scan(&item.URL, &item.Depth, &itemData)
	if err != nil {
		return URLItem{}, false
	}
	if itemData != nil {
		json.Unmarshal(itemData, &item)
	}
	return item, true
}

// Size returns the number of items in the queue.
func (q *PostgresQueue) Size() int {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	var count int64
	err := q.pool.QueryRow(opCtx, `SELECT COUNT(*) FROM crawl_queue`).Scan(&count)
	if err != nil {
		return 0
	}
	return int(count)
}

// HasSeen reports whether the URL has been marked as seen.
func (q *PostgresQueue) HasSeen(url string) bool {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	var exists bool
	err := q.pool.QueryRow(opCtx, `SELECT seen FROM crawl_queue WHERE url = $1`, url).Scan(&exists)
	if err != nil {
		return false
	}
	return exists
}

// MarkSeen records the URL as seen in the queue table.
func (q *PostgresQueue) MarkSeen(url string) {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	if _, err := q.pool.Exec(opCtx, `INSERT INTO crawl_queue (url, depth, item_data, seen) VALUES ($1, 0, '{}', TRUE) ON CONFLICT (url) DO UPDATE SET seen = TRUE`, url); err != nil {
		util.LogError("failed to mark URL seen in postgres", err, zap.String("url", url))
	}
}

// Items returns all queued items ordered by depth.
func (q *PostgresQueue) Items() []URLItem {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	rows, err := q.pool.Query(opCtx, `SELECT item_data FROM crawl_queue ORDER BY depth ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	items := make([]URLItem, 0)
	for rows.Next() {
		var itemData []byte
		if err := rows.Scan(&itemData); err == nil {
			var item URLItem
			if err := json.Unmarshal(itemData, &item); err == nil {
				items = append(items, item)
			}
		}
	}
	return items
}

// AllVisited returns the set of URLs marked as seen.
func (q *PostgresQueue) AllVisited() map[string]bool {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	rows, err := q.pool.Query(opCtx, `SELECT url FROM crawl_queue WHERE seen = TRUE`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	result := make(map[string]bool)
	for rows.Next() {
		var url string
		if rows.Scan(&url) == nil {
			result[url] = true
		}
	}
	return result
}

// Snapshot returns a consistent snapshot of the queue contents and visited set.
func (q *PostgresQueue) Snapshot() ([]URLItem, map[string]bool) {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 10*time.Second)
	defer opCancel()
	tx, err := q.pool.Begin(opCtx)
	if err != nil {
		return q.Items(), q.AllVisited()
	}
	defer tx.Rollback(opCtx)

	items := make([]URLItem, 0)
	rows, err := tx.Query(opCtx, `SELECT item_data FROM crawl_queue ORDER BY depth ASC`)
	if err == nil {
		for rows.Next() {
			var itemData []byte
			if rows.Scan(&itemData) == nil {
				var item URLItem
				if json.Unmarshal(itemData, &item) == nil {
					items = append(items, item)
				}
			}
		}
		rows.Close()
	}

	visited := make(map[string]bool)
	rows, err = tx.Query(opCtx, `SELECT url FROM crawl_queue WHERE seen = TRUE`)
	if err == nil {
		for rows.Next() {
			var url string
			if rows.Scan(&url) == nil {
				visited[url] = true
			}
		}
		rows.Close()
	}

	return items, visited
}

// LoadFromCheckpoint replaces the queue contents with the given items and visited set.
func (q *PostgresQueue) LoadFromCheckpoint(items []URLItem, visited map[string]bool) {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 10*time.Second)
	defer opCancel()
	tx, err := q.pool.Begin(opCtx)
	if err != nil {
		util.LogError("postgres checkpoint load: begin tx failed", err)
		return
	}
	defer tx.Rollback(opCtx)

	if _, err := tx.Exec(opCtx, `DELETE FROM crawl_queue`); err != nil {
		util.LogError("postgres checkpoint load: delete failed", err)
		return
	}

	batch := &pgx.Batch{}
	for _, item := range items {
		itemData, err := json.Marshal(item)
		if err != nil {
			continue
		}
		batch.Queue(`INSERT INTO crawl_queue (url, depth, item_data, seen) VALUES ($1, $2, $3, TRUE)`, item.URL, item.Depth, itemData)
	}
	for url := range visited {
		batch.Queue(`INSERT INTO crawl_queue (url, depth, item_data, seen) VALUES ($1, 0, '{}', TRUE) ON CONFLICT (url) DO NOTHING`, url)
	}

	br := tx.SendBatch(opCtx, batch)
	if err := br.Close(); err != nil {
		util.LogError("postgres checkpoint load: batch failed", err)
		return
	}

	if err := tx.Commit(opCtx); err != nil {
		util.LogError("postgres checkpoint load: commit failed", err)
	}
}

// Close releases the underlying Postgres connection pool.
func (q *PostgresQueue) Close() error {
	q.pool.Close()
	return nil
}
