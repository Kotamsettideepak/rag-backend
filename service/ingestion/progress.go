package ingestion

import (
	"fmt"
	"sync"
	"time"

	"gin-backend/model"
)

func (m *Manager) setJobStage(jobID, stage string) {
	m.updateJob(jobID, func(j *model.UploadJob) {
		j.Stage = stage
		j.Summary = stageLabel(stage)
		j.UpdatedAt = time.Now().UTC()
	})
}

func (m *Manager) setJobStageDetailed(jobID, stage string, file model.StagedFile, idx, total, pct int) {
	m.updateJob(jobID, func(j *model.UploadJob) {
		j.Stage = stage
		j.Summary = stageLabel(stage)
		j.CurrentFile = file.OriginalName
		j.CurrentKind = file.DetectedKind
		j.ProgressLabel = fmt.Sprintf("%d of %d files in progress", idx+1, maxInt(total, 1))
		j.ProgressPercent = pct
		j.UpdatedAt = time.Now().UTC()
	})
}

func (m *Manager) startVideoProgress(jobID string, file model.StagedFile, idx, total int) func() {
	m.setJobStageDetailed(jobID, "converting", file, idx, total, 20)
	done := make(chan struct{})
	go func() {
		t := time.NewTimer(2 * time.Second)
		defer t.Stop()
		select {
		case <-t.C:
			m.setJobStageDetailed(jobID, "transcribing", file, idx, total, 32)
		case <-done:
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

func clampPct(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
