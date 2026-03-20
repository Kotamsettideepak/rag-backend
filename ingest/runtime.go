package ingest

import "sync"

var (
	defaultManager     *Manager
	defaultManagerOnce sync.Once
)

func DefaultManager() *Manager {
	defaultManagerOnce.Do(func() {
		defaultManager = NewManager()
	})
	return defaultManager
}
