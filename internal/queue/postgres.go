package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresQueue struct {
	pool    *pgxpool.Pool
	key     string
	maxSize int
}

func NewPostgresQueue(pgDSN string) (*PostgresQueue, error) {
	return NewPostgresQueueWithSize(pgDSN, DefaultMaxQueueSize)
}

func NewPostgresQueueWithSize(pgDSN string, maxSize int) (*PostgresQueue, error) {
	pool, err := pgxpool.New(context.Background(), pgDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}
	_, err = pool.Exec(context.Background(), `
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
	_, err = pool.Exec(context.Background(), `
		CREATE INDEX IF NOT EXISTS idx_crawl_queue_depth ON crawl_queue(depth)
	`)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to create index: %w", err)
	}
	return &PostgresQueue{
		pool:    pool,
		key:     "crawl_queue",
		maxSize: maxSize,
	}, nil
}

func (q *PostgresQueue) PushURL(url string, depth int) bool {
	if q.HasSeen(url) {
		return false
	}
	if q.Size() >= q.maxSize {
		return false
	}
	item := URLItem{URL: url, Depth: depth}
	itemData, err := json.Marshal(item)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = q.pool.Exec(ctx, `INSERT INTO crawl_queue (url, depth, item_data, seen) VALUES ($1, $2, $3, TRUE)`, url, depth, itemData)
	return err == nil
}

func (q *PostgresQueue) PopURL() (URLItem, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var item URLItem
	var itemData []byte
	err := q.pool.QueryRow(ctx, `SELECT url, depth, item_data FROM crawl_queue ORDER BY depth ASC LIMIT 1`).Scan(&item.URL, &item.Depth, &itemData)
	if err != nil {
		if err == sql.ErrNoRows {
			return URLItem{}, false
		}
		return URLItem{}, false
	}
	_, err = q.pool.Exec(ctx, `DELETE FROM crawl_queue WHERE url = $1`, item.URL)
	if err != nil {
		return URLItem{}, false
	}
	return item, true
}

func (q *PostgresQueue) Size() int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var count int64
	err := q.pool.QueryRow(ctx, `SELECT COUNT(*) FROM crawl_queue`).Scan(&count)
	if err != nil {
		return 0
	}
	return int(count)
}

func (q *PostgresQueue) HasSeen(url string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var exists bool
	err := q.pool.QueryRow(ctx, `SELECT seen FROM crawl_queue WHERE url = $1`, url).Scan(&exists)
	if err != nil {
		return false
	}
	return exists
}

func (q *PostgresQueue) MarkSeen(url string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = q.pool.Exec(ctx, `INSERT INTO crawl_queue (url, depth, item_data, seen) VALUES ($1, 0, '{}', TRUE) ON CONFLICT (url) DO UPDATE SET seen = TRUE`, url)
}

func (q *PostgresQueue) Items() []URLItem {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := q.pool.Query(ctx, `SELECT item_data FROM crawl_queue ORDER BY depth ASC`)
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := q.pool.Query(ctx, `SELECT url FROM crawl_queue WHERE seen = TRUE`)
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = q.pool.Exec(ctx, `DELETE FROM crawl_queue`)
	batch := &pgx.Batch{}
	for _, item := range items {
		itemData, _ := json.Marshal(item)
		batch.Queue(`INSERT INTO crawl_queue (url, depth, item_data, seen) VALUES ($1, $2, $3, TRUE)`, item.URL, item.Depth, itemData)
	}
	for url := range visited {
		batch.Queue(`INSERT INTO crawl_queue (url, depth, item_data, seen) VALUES ($1, 0, '{}', TRUE) ON CONFLICT (url) DO NOTHING`, url)
	}
	_ = q.pool.SendBatch(ctx, batch)
}

func (q *PostgresQueue) Close() error {
	q.pool.Close()
	return nil
}