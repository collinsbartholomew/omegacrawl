package pool

import (
	"bytes"
	"strings"
	"sync"
)

var (
	// BytePool4K is a shared pool of byte buffers sized for 4KB.
	BytePool4K = NewBytePool(4096)
	// BytePool64K is a shared pool of byte buffers sized for 64KB.
	BytePool64K = NewBytePool(65536)
	// BytePool1M is a shared pool of byte buffers sized for 1MB.
	BytePool1M = NewBytePool(1048576)
	// StrBuilder is a shared pool of strings.Builder instances.
	StrBuilder = NewStringBuilderPool()
	// MapStrInt is a shared pool of map[string]int with a hint of 32.
	MapStrInt = NewMapStrIntPool(32)
	// MapStrStr is a shared pool of map[string]string with a hint of 32.
	MapStrStr = NewMapStrStrPool(32)
)

// BytePool is a sync.Pool of byte buffers each with the given capacity.
type BytePool struct {
	pool sync.Pool
	size int
}

// NewBytePool creates a BytePool that hands out buffers with the given initial capacity.
func NewBytePool(size int) *BytePool {
	return &BytePool{
		size: size,
		pool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 0, size)
				return &buf
			},
		},
	}
}

// Get returns a zero-length byte slice from the pool.
func (p *BytePool) Get() []byte {
	buf := p.pool.Get().(*[]byte)
	*buf = (*buf)[:0]
	return *buf
}

// Put returns a buffer to the pool unless it grew beyond twice the pool size.
func (p *BytePool) Put(buf []byte) {
	if cap(buf) <= p.size*2 {
		p.pool.Put(&buf)
	}
}

// StringBuilderPool is a sync.Pool of strings.Builder instances.
type StringBuilderPool struct {
	pool sync.Pool
}

// NewStringBuilderPool creates a StringBuilderPool.
func NewStringBuilderPool() *StringBuilderPool {
	return &StringBuilderPool{
		pool: sync.Pool{
			New: func() interface{} {
				return new(strings.Builder)
			},
		},
	}
}

// Get returns a strings.Builder from the pool.
func (p *StringBuilderPool) Get() *strings.Builder {
	return p.pool.Get().(*strings.Builder)
}

// Put resets and returns a strings.Builder to the pool.
func (p *StringBuilderPool) Put(sb *strings.Builder) {
	sb.Reset()
	p.pool.Put(sb)
}

// MapStrIntPool is a sync.Pool of map[string]int with an initial capacity hint.
type MapStrIntPool struct {
	pool sync.Pool
	hint int
}

// NewMapStrIntPool creates a MapStrIntPool with the given capacity hint.
func NewMapStrIntPool(hint int) *MapStrIntPool {
	return &MapStrIntPool{
		hint: hint,
		pool: sync.Pool{
			New: func() interface{} {
				return make(map[string]int, hint)
			},
		},
	}
}

// Get returns an empty map[string]int from the pool.
func (p *MapStrIntPool) Get() map[string]int {
	m := p.pool.Get().(map[string]int)
	for k := range m {
		delete(m, k)
	}
	return m
}

// Put returns a map to the pool unless it grew beyond 10000 entries.
func (p *MapStrIntPool) Put(m map[string]int) {
	if len(m) < 10000 {
		p.pool.Put(m)
	}
}

// MapStrStrPool is a sync.Pool of map[string]string with an initial capacity hint.
type MapStrStrPool struct {
	pool sync.Pool
	hint int
}

// NewMapStrStrPool creates a MapStrStrPool with the given capacity hint.
func NewMapStrStrPool(hint int) *MapStrStrPool {
	return &MapStrStrPool{
		hint: hint,
		pool: sync.Pool{
			New: func() interface{} {
				return make(map[string]string, hint)
			},
		},
	}
}

// Get returns an empty map[string]string from the pool.
func (p *MapStrStrPool) Get() map[string]string {
	m := p.pool.Get().(map[string]string)
	for k := range m {
		delete(m, k)
	}
	return m
}

// Put returns a map to the pool unless it grew beyond 10000 entries.
func (p *MapStrStrPool) Put(m map[string]string) {
	if len(m) < 10000 {
		p.pool.Put(m)
	}
}

// GlobalBufferPool is the shared pool of bytes.Buffer instances.
var GlobalBufferPool = NewBufferPool()

// BufferPool is a sync.Pool of bytes.Buffer instances.
type BufferPool struct {
	pool sync.Pool
}

// NewBufferPool creates a BufferPool.
func NewBufferPool() *BufferPool {
	return &BufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}
}

// Get returns a bytes.Buffer from the pool.
func (p *BufferPool) Get() *bytes.Buffer {
	return p.pool.Get().(*bytes.Buffer)
}

// Put resets and returns a bytes.Buffer to the pool.
func (p *BufferPool) Put(buf *bytes.Buffer) {
	buf.Reset()
	p.pool.Put(buf)
}
