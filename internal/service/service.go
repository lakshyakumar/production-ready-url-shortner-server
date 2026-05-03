package service

import (
	"github/lakshyakumar/production-ready-url-shortner-server/internal/platform/config"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/repository"

	"github.com/redis/go-redis/v9"
)

// Deps is the set of external infrastructure handed to the service layer.
// Add new infra (Redis, HTTP clients, message brokers) here as it appears.
type Deps struct {
	// Router routes DB calls to the right pool (primary vs replica) with
	// per-pool circuit breakers. Repos consume it via repository.Router.
	Router repository.Router
	// Redis backs the distributed rate limiter. Optional — when nil, the
	// rate limiter is left unwired and middleware should not be installed.
	Redis  *redis.Client
	Config *config.Config
}

// Services is the typed aggregate of every service in the application.
// Handlers, crons and workers consume fields from this struct rather than
// constructing services themselves.
type Services struct {
	URL         URLService
	Cleanup     CleanupService
	RateLimiter RateLimiter
}

// Initializable is implemented by services that need peer-service dependencies
// resolved after all services have been constructed (two-phase init). Use this
// only for true peer cycles — prefer constructor injection otherwise.
type Initializable interface {
	Init(*Services)
}

// New builds the full service graph in two phases:
//  1. Construct every service with its infrastructure deps (repos, config, etc.).
//  2. For any service that implements Initializable, call Init so it can grab
//     references to its peer services from the fully-built Services struct.
func New(d Deps) (*Services, error) {
	urlRepo := repository.NewURLRepository(d.Router)
	reqRepo := repository.NewRequestRepository(d.Router)

	s := &Services{
		URL:     NewURLService(urlRepo, reqRepo),
		Cleanup: NewCleanupService(urlRepo, d.Config),
	}

	if d.Redis != nil && d.Config != nil {
		s.RateLimiter = NewRedisRateLimiter(
			d.Redis,
			d.Config.RateLimitPerMinute,
			d.Config.RateLimitWindow,
			d.Config.RedisOpTimeout,
		)
	}

	// Phase 2: peer wiring. Add new services to this slice as they're added
	// to the struct above. Services that don't need peer deps simply don't
	// implement Initializable and are skipped.
	for _, svc := range []any{s.URL, s.Cleanup} {
		if init, ok := svc.(Initializable); ok {
			init.Init(s)
		}
	}
	return s, nil
}
