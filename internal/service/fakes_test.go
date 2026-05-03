package service_test

import (
	"context"
	"sync/atomic"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/model"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/repository"

	"github.com/google/uuid"
)

// fakeURLRepo implements repository.URLRepository with per-method function
// fields. Tests set only the methods they care about; everything else
// returns a benign default. Call counts are atomic so the fire-and-forget
// goroutine in Redirect() can race without tripping the race detector.
type fakeURLRepo struct {
	CreateFn            func(context.Context, *model.URL) error
	GetByShortKeyFn     func(context.Context, string) (*model.URL, error)
	UpdateLastUsedFn    func(context.Context, string) error
	DeleteUnusedSinceFn func(context.Context, time.Time) (int64, error)

	CreateCalls            atomic.Int32
	GetByShortKeyCalls     atomic.Int32
	UpdateLastUsedCalls    atomic.Int32
	DeleteUnusedSinceCalls atomic.Int32
}

func (f *fakeURLRepo) Create(ctx context.Context, u *model.URL) error {
	f.CreateCalls.Add(1)
	if f.CreateFn != nil {
		return f.CreateFn(ctx, u)
	}
	return nil
}

func (f *fakeURLRepo) GetByShortKey(ctx context.Context, key string) (*model.URL, error) {
	f.GetByShortKeyCalls.Add(1)
	if f.GetByShortKeyFn != nil {
		return f.GetByShortKeyFn(ctx, key)
	}
	return nil, repository.ErrNotFound
}

func (f *fakeURLRepo) UpdateLastUsed(ctx context.Context, id string) error {
	f.UpdateLastUsedCalls.Add(1)
	if f.UpdateLastUsedFn != nil {
		return f.UpdateLastUsedFn(ctx, id)
	}
	return nil
}

func (f *fakeURLRepo) DeleteUnusedSince(ctx context.Context, cutoff time.Time) (int64, error) {
	f.DeleteUnusedSinceCalls.Add(1)
	if f.DeleteUnusedSinceFn != nil {
		return f.DeleteUnusedSinceFn(ctx, cutoff)
	}
	return 0, nil
}

// fakeRequestRepo implements repository.RequestRepository.
type fakeRequestRepo struct {
	LogCreateRequestFn func(context.Context, uuid.UUID, string) (*model.IncomingRequest, error)

	LogCreateRequestCalls atomic.Int32
}

func (f *fakeRequestRepo) LogCreateRequest(ctx context.Context, urlID uuid.UUID, agent string) (*model.IncomingRequest, error) {
	f.LogCreateRequestCalls.Add(1)
	if f.LogCreateRequestFn != nil {
		return f.LogCreateRequestFn(ctx, urlID, agent)
	}
	return &model.IncomingRequest{
		Base:      model.NewBase(),
		URLID:     urlID,
		IPAddress: "fake",
		UserAgent: agent,
	}, nil
}
