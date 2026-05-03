package service

import "context"

type CleanupService interface {
	RunCleanup(ctx context.Context) error
}
