package queue

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
)

type KafkaQueue struct {
	writer    *kafka.Writer
	reader    *kafka.Reader
	key       string
	maxSize   int
	seenMu    sync.RWMutex
	seenSet   map[string]bool
	seenTopic string
	brokers   []string
}

func NewKafkaQueue(kafkaURL string) (*KafkaQueue, error) {
	return NewKafkaQueueWithSize(kafkaURL, DefaultMaxQueueSize)
}

func NewKafkaQueueWithSize(kafkaURL string, maxSize int) (*KafkaQueue, error) {
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

	q := &KafkaQueue{
		writer:    writer,
		reader:    reader,
		key:       "crawl:queue",
		maxSize:   maxSize,
		seenSet:   make(map[string]bool),
		seenTopic: "crawl_seen",
		brokers:   []string{kafkaURL},
	}

	go q.consumeSeenTopic()

	return q, nil
}

func (q *KafkaQueue) consumeSeenTopic() {
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
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		msg, err := seenReader.FetchMessage(ctx)
		cancel()
		if err != nil {
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
		seenReader.CommitMessages(ctx, msg)
	}
}

func (q *KafkaQueue) PushURL(url string, depth int) bool {
	if q.HasSeen(url) {
		return false
	}
	if q.Size() >= q.maxSize {
		return false
	}
	item := URLItem{URL: url, Depth: depth}
	data, err := json.Marshal(item)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = q.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(url),
		Value: data,
	})
	if err != nil {
		return false
	}
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
	data, _ := json.Marshal(item)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q.writer.WriteMessages(ctx, kafka.Message{
		Topic: q.seenTopic,
		Key:   []byte(url),
		Value: data,
	})
}

func (q *KafkaQueue) PopURL() (URLItem, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msg, err := q.reader.FetchMessage(ctx)
	if err != nil {
		return URLItem{}, false
	}
	var item URLItem
	if err := json.Unmarshal(msg.Value, &item); err != nil {
		return URLItem{}, false
	}
	return item, true
}

func (q *KafkaQueue) Size() int {
	q.seenMu.RLock()
	defer q.seenMu.RUnlock()
	return len(q.seenSet)
}

func (q *KafkaQueue) HasSeen(url string) bool {
	q.seenMu.RLock()
	defer q.seenMu.RUnlock()
	return q.seenSet[url]
}

func (q *KafkaQueue) MarkSeen(url string) {
	q.markSeen(url)
}

func (q *KafkaQueue) Items() []URLItem {
	q.seenMu.RLock()
	defer q.seenMu.RUnlock()
	items := make([]URLItem, 0, len(q.seenSet))
	for url := range q.seenSet {
		items = append(items, URLItem{URL: url})
	}
	return items
}

func (q *KafkaQueue) AllVisited() map[string]bool {
	q.seenMu.RLock()
	defer q.seenMu.RUnlock()
	result := make(map[string]bool, len(q.seenSet))
	for k, v := range q.seenSet {
		result[k] = v
	}
	return result
}

func (q *KafkaQueue) LoadFromCheckpoint(items []URLItem, visited map[string]bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, item := range items {
		data, _ := json.Marshal(item)
		q.writer.WriteMessages(ctx, kafka.Message{
			Key:   []byte(item.URL),
			Value: data,
		})
	}

	for url := range visited {
		q.markSeen(url)
	}
}

func (q *KafkaQueue) Close() error {
	q.writer.Close()
	q.reader.Close()
	return nil
}