package embedding

import "sync"

type Cache struct {
	mu    sync.RWMutex
	items map[string][]float64
}

func NewCache() *Cache {
	return &Cache{
		items: make(map[string][]float64),
	}
}

func (c *Cache) Get(key string) ([]float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	value, ok := c.items[key]
	if !ok {
		return nil, false
	}

	cloned := make([]float64, len(value))
	copy(cloned, value)
	return cloned, true
}

func (c *Cache) Set(key string, value []float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cloned := make([]float64, len(value))
	copy(cloned, value)
	c.items[key] = cloned
}
