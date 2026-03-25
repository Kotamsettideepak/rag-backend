package ingestion

import "gin-backend/model"

func (m *Manager) updateJob(jobID string, update func(*model.UploadJob)) {
	m.mu.Lock()
	job, ok := m.jobs[jobID]
	if !ok {
		m.mu.Unlock()
		return
	}
	update(job)
	snapshot := cloneJob(job)
	subs := copySubscribers(m.jobSubs[jobID])
	m.mu.Unlock()
	m.publish(snapshot, subs)
}

func (m *Manager) updateFile(jobID, fileID string, update func(*model.FileResult)) {
	m.mu.Lock()
	job, ok := m.jobs[jobID]
	if !ok {
		m.mu.Unlock()
		return
	}
	for i := range job.Files {
		if job.Files[i].FileID == fileID {
			update(&job.Files[i])
			snapshot := cloneJob(job)
			subs := copySubscribers(m.jobSubs[jobID])
			m.mu.Unlock()
			m.publish(snapshot, subs)
			return
		}
	}
	m.mu.Unlock()
}

func (m *Manager) publish(job *model.UploadJob, subs []chan *model.UploadJob) {
	for _, ch := range subs {
		select {
		case ch <- cloneJob(job):
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- cloneJob(job):
			default:
			}
		}
	}
}

func copySubscribers(src map[string]chan *model.UploadJob) []chan *model.UploadJob {
	out := make([]chan *model.UploadJob, 0, len(src))
	for _, ch := range src {
		out = append(out, ch)
	}
	return out
}
