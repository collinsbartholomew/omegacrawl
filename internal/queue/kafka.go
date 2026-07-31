package queue

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/user/clone/internal/util"
)

// KafkaQueue is a Kafka-backed queue with a local seen set for deduplication.
type KafkaQueue struct {
	writer     *kafka.Writer
	reader     *kafka.Reader
	key        string
	maxSize    int
	seenMu     sync.RWMutex
	seenSet    map[string]bool
	seenTopic  string
	brokers    []string
	closeCh    chan struct{}
	wg         sync.WaitGroup
	pending    atomic.Int64
	parentCtx  context.Context
	seenCtx    context.Context
	seenCancel context.CancelFunc
}

// NewKafkaQueue creates a KafkaQueue using the default max queue size.
func NewKafkaQueue(ctx context.Context, kafkaURL string) (*KafkaQueue, error) {
	return NewKafkaQueueWithSize(ctx, kafkaURL, DefaultMaxQueueSize)
}

// NewKafkaQueueWithSize creates a KafkaQueue with the given maxSize and starts
// a background consumer of the seen topic.
func NewKafkaQueueWithSize(ctx context.Context, kafkaURL string, maxSize int) (*KafkaQueue, error) {
	writer := &kafka.Writer{
		Addr:     kafka.TCP(kafkaURL),
		Topic:    "crawl_queue",
		Balancer: &kafka.LeastBytes{},
	}
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        []string{kafkaURL},
		Topic:          "crawl_queue",
		GroupID:        "crawler",
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: 1 * time.Second,
	})

	seenCtx, seenCancel := context.WithCancel(ctx)
	q := &KafkaQueue{
		writer:     writer,
		reader:     reader,
		key:        "crawl:queue",
		maxSize:    maxSize,
		seenSet:    make(map[string]bool),
		seenTopic:  "crawl_seen",
		brokers:    []string{kafkaURL},
		closeCh:    make(chan struct{}),
		parentCtx:  ctx,
		seenCtx:    seenCtx,
		seenCancel: seenCancel,
	}

	q.wg.Add(1)
	go q.consumeSeenTopic()

	return q, nil
}

func (q *KafkaQueue) consumeSeenTopic() {
	defer q.wg.Done()
	seenReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        q.brokers,
		Topic:          q.seenTopic,
		GroupID:        "crawler-seen",
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: 1 * time.Second,
		StartOffset:    kafka.LastOffset,
	})
	defer seenReader.Close()

	for {
		select {
		case <-q.closeCh:
			return
		default:
		}

		msg, err := seenReader.FetchMessage(q.seenCtx)
		if err != nil {
			select {
			case <-q.closeCh:
				return
			default:
			}
			continue
		}
		if msg.Value != nil {
			var item URLItem
			if json.Unmarshal(msg.Value, &item) == nil {
				q.seenMu.Lock()
				q.seenSet[item.URL] = true
				q.seenMu.Unlock()
			}
		}
		commitCtx, commitCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
		if err := seenReader.CommitMessages(commitCtx, msg); err != nil {
			util.LogError("failed to commit seen message in kafka", err)
		}
		commitCancel()
	}
}

// Close stops the seen topic consumer and closes the Kafka writer and reader.
func (q *KafkaQueue) Close() error {
	q.seenCancel()
	close(q.closeCh)
	q.wg.Wait()
	q.writer.Close()
	q.reader.Close()
	return nil
}

// PushURL enqueues url at depth unless it was already seen or the queue is full.
func (q *KafkaQueue) PushURL(url string, depth int) bool {
	if q.HasSeen(url) {
		return false
	}
	if q.pending.Load() >= int64(q.maxSize) {
		return false
	}
	item := URLItem{URL: url, Depth: depth}
	data, err := json.Marshal(item)
	if err != nil {
		return false
	}
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	err = q.writer.WriteMessages(opCtx, kafka.Message{
		Key:   []byte(url),
		Value: data,
	})
	if err != nil {
		return false
	}
	q.pending.Add(1)
	q.markSeen(url)
	return true
}

func (q *KafkaQueue) markSeen(url string) {
	q.seenMu.Lock()
	if q.seenSet[url] {
		q.seenMu.Unlock()
		return
	}
	q.seenSet[url] = true
	q.seenMu.Unlock()

	item := URLItem{URL: url, Depth: 0}
	data, err := json.Marshal(item)
	if err != nil {
		util.LogError("failed to marshal seen URL item", err, zap.String("url", url))
		data = []byte("null")
	}
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	q.writer.WriteMessages(opCtx, kafka.Message{
		Topic: q.seenTopic,
		Key:   []byte(url),
		Value: data,
	})
}

// PopURL fetches and commits the next message from the queue.
func (q *KafkaQueue) PopURL() (URLItem, bool) {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer opCancel()
	msg, err := q.reader.FetchMessage(opCtx)
	if err != nil {
		return URLItem{}, false
	}
	var item URLItem
	if err := json.Unmarshal(msg.Value, &item); err != nil {
		return URLItem{}, false
	}
	commitCtx, commitCancel := context.WithTimeout(q.parentCtx, 5*time.Second)
	defer commitCancel()
	if err := q.reader.CommitMessages(commitCtx, msg); err != nil {
		return URLItem{}, false
	}
	q.pending.Add(-1)
	return item, true
}

// Size returns the number of pending items in the queue.
func (q *KafkaQueue) Size() int {
	n := q.pending.Load()
	if n < 0 {
		return 0
	}
	return int(n)
}

// HasSeen reports whether the URL has been marked as seen.
func (q *KafkaQueue) HasSeen(url string) bool {
	q.seenMu.RLock()
	defer q.seenMu.RUnlock()
	return q.seenSet[url]
}

// MarkSeen records the URL as seen locally and publishes it to the seen topic.
func (q *KafkaQueue) MarkSeen(url string) {
	q.markSeen(url)
}

// Items returns the seen URLs as URLItems.
func (q *KafkaQueue) Items() []URLItem {
	q.seenMu.RLock()
	defer q.seenMu.RUnlock()
	items := make([]URLItem, 0, len(q.seenSet))
	for url := range q.seenSet {
		items = append(items, URLItem{URL: url})
	}
	return items
}

// AllVisited returns a copy of the seen URL set.
func (q *KafkaQueue) AllVisited() map[string]bool {
	q.seenMu.RLock()
	defer q.seenMu.RUnlock()
	result := make(map[string]bool, len(q.seenSet))
	for k, v := range q.seenSet {
		result[k] = v
	}
	return result
}

// Snapshot returns a consistent snapshot of the queue contents and visited set.
func (q *KafkaQueue) Snapshot() ([]URLItem, map[string]bool) {
	q.seenMu.RLock()
	defer q.seenMu.RUnlock()
	items := make([]URLItem, 0, len(q.seenSet))
	for url := range q.seenSet {
		items = append(items, URLItem{URL: url})
	}
	result := make(map[string]bool, len(q.seenSet))
	for k, v := range q.seenSet {
		result[k] = v
	}
	return items, result
}

// LoadFromCheckpoint enqueues the given items and marks the visited URLs as seen.
func (q *KafkaQueue) LoadFromCheckpoint(items []URLItem, visited map[string]bool) {
	opCtx, opCancel := context.WithTimeout(q.parentCtx, 30*time.Second)
	defer opCancel()

	for _, item := range items {
		data, err := json.Marshal(item)
		if err != nil {
			util.LogError("failed to marshal checkpoint item", err, zap.String("url", item.URL))
			data = []byte("null")
		}
		q.writer.WriteMessages(opCtx, kafka.Message{
			Key:   []byte(item.URL),
			Value: data,
		})
		q.pending.Add(1)
	}

	for url := range visited {
		q.markSeen(url)
	}
}
