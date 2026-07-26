package pool

import (
	"bytes"
	"strings"
	"sync"
)

var (
	BytePool4K   = NewBytePool(4096)
	BytePool64K  = NewBytePool(65536)
	BytePool1M   = NewBytePool(1048576)
	StrBuilder   = NewStringBuilderPool()
	MapStrInt    = NewMapStrIntPool(32)
	MapStrStr    = NewMapStrStrPool(32)
)

type BytePool struct {
	pool    sync.Pool
	size    int
}

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

func (p *BytePool) Get() []byte {
	buf := p.pool.Get().(*[]byte)
	*buf = (*buf)[:0]
	return *buf
}

func (p *BytePool) Put(buf []byte) {
	if cap(buf) <= p.size*2 {
		p.pool.Put(&buf)
	}
}

type StringBuilderPool struct {
	pool sync.Pool
}

func NewStringBuilderPool() *StringBuilderPool {
	return &StringBuilderPool{
		pool: sync.Pool{
			New: func() interface{} {
				return new(strings.Builder)
			},
		},
	}
}

func (p *StringBuilderPool) Get() *strings.Builder {
	return p.pool.Get().(*strings.Builder)
}

func (p *StringBuilderPool) Put(sb *strings.Builder) {
	sb.Reset()
	p.pool.Put(sb)
}

type MapStrIntPool struct {
	pool  sync.Pool
	hint  int
}

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

func (p *MapStrIntPool) Get() map[string]int {
	m := p.pool.Get().(map[string]int)
	for k := range m {
		delete(m, k)
	}
	return m
}

func (p *MapStrIntPool) Put(m map[string]int) {
	if len(m) < 10000 {
		p.pool.Put(m)
	}
}

type MapStrStrPool struct {
	pool sync.Pool
	hint int
}

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

func (p *MapStrStrPool) Get() map[string]string {
	m := p.pool.Get().(map[string]string)
	for k := range m {
		delete(m, k)
	}
	return m
}

func (p *MapStrStrPool) Put(m map[string]string) {
	if len(m) < 10000 {
		p.pool.Put(m)
	}
}

var GlobalBufferPool = NewBufferPool()

type BufferPool struct {
	pool sync.Pool
}

func NewBufferPool() *BufferPool {
	return &BufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}
}

func (p *BufferPool) Get() *bytes.Buffer {
	return p.pool.Get().(*bytes.Buffer)
}

func (p *BufferPool) Put(buf *bytes.Buffer) {
	buf.Reset()
	p.pool.Put(buf)
}
