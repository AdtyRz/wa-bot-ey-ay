package queue

import (
	"context"
	"log/slog"
	"sync"
)

type Job func(ctx context.Context)

type WorkerPool struct {
	maxWorkers int
	jobQueue   chan Job
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewWorkerPool(workers int, queueCapacity int) *WorkerPool {
	if workers <= 0 {
		workers = 5
	}
	if queueCapacity <= 0 {
		queueCapacity = 100
	}

	ctx, cancel := context.WithCancel(context.Background())
	pool := &WorkerPool{
		maxWorkers: workers,
		jobQueue:   make(chan Job, queueCapacity),
		ctx:        ctx,
		cancel:     cancel,
	}

	pool.start()
	return pool
}

func (p *WorkerPool) start() {
	for i := 0; i < p.maxWorkers; i++ {
		p.wg.Add(1)
		go func(workerID int) {
			defer p.wg.Done()
			for {
				select {
				case <-p.ctx.Done():
					return
				case job, ok := <-p.jobQueue:
					if !ok {
						return
					}
					// Run job safely
					func() {
						defer func() {
							if r := recover(); r != nil {
								slog.Error("Worker recovered from panic", "worker", workerID, "error", r)
							}
						}()
						job(p.ctx)
					}()
				}
			}
		}(i)
	}
}

// Submit queues a job; returns false if queue is full (backpressure)
func (p *WorkerPool) Submit(job Job) bool {
	select {
	case p.jobQueue <- job:
		return true
	default:
		// Queue full
		return false
	}
}

// Stop gracefully shuts down the worker pool
func (p *WorkerPool) Stop() {
	p.cancel()
	close(p.jobQueue)
	p.wg.Wait()
}
