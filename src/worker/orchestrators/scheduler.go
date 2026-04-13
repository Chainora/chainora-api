package orchestrators

import (
	"context"
	"log"
	"sync"
	"time"

	"chainora-api/worker/jobs"
)

type Scheduler struct {
	interval time.Duration
	jobs     []jobs.Job
}

func NewScheduler(interval time.Duration, jobsList ...jobs.Job) *Scheduler {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Scheduler{interval: interval, jobs: jobsList}
}

func (s *Scheduler) Run(ctx context.Context) {
	s.runOnce(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[worker] scheduler stopped")
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *Scheduler) runOnce(ctx context.Context) {
	if len(s.jobs) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, job := range s.jobs {
		if job == nil {
			continue
		}

		wg.Add(1)
		go func(j jobs.Job) {
			defer wg.Done()
			if err := j.Run(ctx); err != nil {
				log.Printf("[worker][%s] failed: %v", j.Name(), err)
			}
		}(job)
	}
	wg.Wait()
}
