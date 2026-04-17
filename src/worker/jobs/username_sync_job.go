package jobs

import (
	"context"
	"log"
	"strings"

	"chainora-api/worker/scanners"
)

type UsernameAddressSource interface {
	ListAddresses(ctx context.Context) ([]string, error)
}

type UsernameSyncJob struct {
	scanner         *scanners.UsernameScanner
	addresses       []string
	source          UsernameAddressSource
	noAddressLogged bool
}

func NewUsernameSyncJob(scanner *scanners.UsernameScanner, addresses []string, source UsernameAddressSource) *UsernameSyncJob {
	return &UsernameSyncJob{scanner: scanner, addresses: addresses, source: source}
}

func (j *UsernameSyncJob) Name() string {
	return "username-sync"
}

func (j *UsernameSyncJob) Run(ctx context.Context) error {
	if j.scanner == nil {
		return nil
	}

	addresses := j.addresses
	if len(addresses) == 0 && j.source != nil {
		dynamicAddresses, err := j.source.ListAddresses(ctx)
		if err != nil {
			log.Printf("[worker][%s] failed to load addresses from source: %v", j.Name(), err)
		} else {
			addresses = dynamicAddresses
		}
	}

	if len(addresses) == 0 {
		if !j.noAddressLogged {
			log.Printf("[worker][%s] skipped: no username_sync.addresses configured and no dynamic addresses discovered", j.Name())
			j.noAddressLogged = true
		}
		return nil
	}
	j.noAddressLogged = false

	for _, address := range addresses {
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
