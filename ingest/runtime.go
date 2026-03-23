package ingest

var defaultManager *Manager

func DefaultManager() *Manager {
	if defaultManager == nil {
		panic("default ingest manager is not configured")
	}
	return defaultManager
}

func SetDefaultManager(manager *Manager) {
	defaultManager = manager
}
