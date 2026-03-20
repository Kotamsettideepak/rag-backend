package worker

import (
	"context"
	"sync"
	"time"

	"gin-backend/embedding"
	"gin-backend/models"
)

type BatchTask struct {
	Ctx      context.Context
	JobID    string
	Batch    []models.Chunk
	Response chan BatchResult
}

type BatchResult struct {
	JobID     string
	Records   []models.VectorRecord
	Processed int
	Failed    int
	Duration  time.Duration
	Err       error
}

type Pool struct {
	embedder    *embedding.Service
	rateLimiter <-chan time.Time
	ticker      *time.Ticker
	jobs        chan BatchTask
	wg          sync.WaitGroup
}

func NewPool(embedder *embedding.Service, workerCount int, queueSize int, ratePerSecond int) *Pool {
	if workerCount <= 0 {
		workerCount = 5
	}
	if queueSize <= 0 {
		queueSize = workerCount * 4
	}

	var limiter <-chan time.Time
	var ticker *time.Ticker
	if ratePerSecond > 0 {
		interval := time.Second / time.Duration(ratePerSecond)
		if interval <= 0 {
			interval = time.Millisecond
		}
		ticker = time.NewTicker(interval)
		limiter = ticker.C
	}

	pool := &Pool{
		embedder:    embedder,
		rateLimiter: limiter,
		ticker:      ticker,
		jobs:        make(chan BatchTask, queueSize),
	}

	for index := 0; index < workerCount; index++ {
		pool.wg.Add(1)
		go pool.runWorker(index + 1)
	}

	return pool
}

func (p *Pool) Submit(task BatchTask) {
	p.jobs <- task
}

func (p *Pool) Shutdown() {
	close(p.jobs)
	p.wg.Wait()
	if p.ticker != nil {
		p.ticker.Stop()
	}
}

func (p *Pool) runWorker(workerID int) {
	defer p.wg.Done()

	for task := range p.jobs {
		started := time.Now()
		if p.rateLimiter != nil {
			select {
			case <-task.Ctx.Done():
				task.Response <- BatchResult{JobID: task.JobID, Failed: len(task.Batch), Err: task.Ctx.Err()}
				continue
			case <-p.rateLimiter:
			}
		}

		records, err := p.embedder.EmbedChunks(task.Ctx, task.Batch)
		result := BatchResult{
			JobID:     task.JobID,
			Records:   records,
			Processed: len(task.Batch),
			Duration:  time.Since(started),
			Err:       err,
		}
		if err != nil {
			result.Failed = len(task.Batch)
		}

		task.Response <- result
	}
}
