package viewer

import (
	"fmt"
	"sync"
	"time"
)

// Cache is a small LRU of documents shared by every copy of a viewer. A nil
// *Cache caches nothing.
type Cache struct {
	mu    sync.Mutex
	max   int
	order []string
	docs  map[string]*Document
}

func NewCache(n int) *Cache {
	return &Cache{max: max(n, 1), docs: map[string]*Document{}}
}

func cacheKey(path string, mtime time.Time, size int64, theme string, tabWidth int) string {
	return fmt.Sprintf("%s\x00%d\x00%d\x00%s\x00%d", path, mtime.UnixNano(), size, theme, tabWidth)
}

func (c *Cache) Get(key string) (*Document, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.docs[key]
	if ok {
		c.touch(key)
	}
	return d, ok
}

func (c *Cache) Put(key string, d *Document) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.docs[key]; !ok && len(c.order) >= c.max {
		delete(c.docs, c.order[0])
		c.order = c.order[1:]
	}
	c.docs[key] = d
	c.touch(key)
}

func (c *Cache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.docs)
}

func (c *Cache) touch(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append(c.order, key)
}
