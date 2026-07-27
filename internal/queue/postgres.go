package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresQueue struct {
	pool      *pgxpool.Pool
	key       string
	maxSize   int
	parentCtx context.Context
}

func NewPostgresQueue(ctx context.Context, pgDSN string) (*PostgresQueue, error) {
	return NewPostgresQueueWithSize(ctx, pgDSN, DefaultMaxQueueSize)
}

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

func (q *PostgresQueue) MarkSeen(url string) {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	_, _ = q.pool.Exec(opCtx, `INSERT INTO crawl_queue (url, depth, item_data, seen) VALUES ($1, 0, '{}', TRUE) ON CONFLICT (url) DO UPDATE SET seen = TRUE`, url)
}

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

func (q *PostgresQueue) LoadFromCheckpoint(items []URLItem, visited map[string]bool) {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 10*time.Second)
	defer opCancel()
	_, _ = q.pool.Exec(opCtx, `DELETE FROM crawl_queue`)
	batch := &pgx.Batch{}
	for _, item := range items {
		itemData, _ := json.Marshal(item)
		batch.Queue(`INSERT INTO crawl_queue (url, depth, item_data, seen) VALUES ($1, $2, $3, TRUE)`, item.URL, item.Depth, itemData)
	}
	for url := range visited {
		batch.Queue(`INSERT INTO crawl_queue (url, depth, item_data, seen) VALUES ($1, 0, '{}', TRUE) ON CONFLICT (url) DO NOTHING`, url)
	}
	_ = q.pool.SendBatch(opCtx, batch)
}

func (q *PostgresQueue) Close() error {
	q.pool.Close()
	return nil
}