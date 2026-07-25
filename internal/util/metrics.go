package util

import (
	"sync/atomic"
)

type Metrics struct {
	PagesFetched    atomic.Int64
	AssetsCaptured  atomic.Int64
	ErrorsCount     atomic.Int64
	BytesDownloaded atomic.Int64
}

func (m *Metrics) IncPagesFetched() {
	m.PagesFetched.Add(1)
}

func (m *Metrics) IncAssetsCaptured() {
	m.AssetsCaptured.Add(1)
}

func (m *Metrics) IncErrors() {
	m.ErrorsCount.Add(1)
}

func (m *Metrics) AddBytes(n int64) {
	m.BytesDownloaded.Add(n)
}

func (m *Metrics) Snapshot() (pages, assets, errors, bytes int64) {
	return m.PagesFetched.Load(), m.AssetsCaptured.Load(), m.ErrorsCount.Load(), m.BytesDownloaded.Load()
}
