package lsp

import "sync"

// Ring is a fixed-size, concurrency-safe buffer that keeps the most recent bytes written.
type Ring struct {
	mu   sync.Mutex
	buf  []byte
	size int
}

func NewRing(size int) *Ring { return &Ring{size: size} }

func (r *Ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(p)
	if n >= r.size {
		r.buf = append(r.buf[:0], p[n-r.size:]...)
		return n, nil
	}
	if over := len(r.buf) + n - r.size; over > 0 {
		r.buf = append(r.buf[:0], r.buf[over:]...)
	}
	r.buf = append(r.buf, p...)
	return n, nil
}

func (r *Ring) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}
