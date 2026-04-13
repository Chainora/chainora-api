package jobs

import (
	"context"
	"log"
	"strings"

	"chainora-api/worker/scanners"
)

type UsernameSyncJob struct {
	scanner   *scanners.UsernameScanner
	addresses []string
}

func NewUsernameSyncJob(scanner *scanners.UsernameScanner, addresses []string) *UsernameSyncJob {
	return &UsernameSyncJob{scanner: scanner, addresses: addresses}
}

func (j *UsernameSyncJob) Name() string {
	return "username-sync"
}

func (j *UsernameSyncJob) Run(ctx context.Context) error {
	if j.scanner == nil {
		return nil
	}

	if len(j.addresses) == 0 {
		log.Printf("[worker][%s] skipped: no WORKER_USERNAME_SYNC_ADDRESSES configured", j.Name())
		return nil
	}

	for _, address := range j.addresses {
		wallet := strings.TrimSpace(address)
		if wallet == "" {
			continue
		}

		username, err := j.scanner.ResolvePrimaryUsername(ctx, wallet)
		if err != nil {
			log.Printf("[worker][%s] address=%s error=%v", j.Name(), wallet, err)
			continue
		}

		log.Printf("[worker][%s] address=%s username=%s", j.Name(), wallet, username)
	}

	return nil
}
