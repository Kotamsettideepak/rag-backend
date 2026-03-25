package ingestion

import "gin-backend/model"

var defaultManager *Manager

func DefaultManager() *Manager {
	if defaultManager == nil {
		panic("default ingestion manager is not initialized")
	}
	return defaultManager
}

func SetDefaultManager(m *Manager) {
	defaultManager = m
}

func cloneJob(j *model.UploadJob) *model.UploadJob {
	if j == nil {
		return nil
	}
	cp := *j
	cp.Files = append([]model.FileResult(nil), j.Files...)
	return &cp
}
