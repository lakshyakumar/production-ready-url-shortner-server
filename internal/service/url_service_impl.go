package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/model"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/repository"
)

const (
	base62Alphabet      = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	shortKeyLength      = 11
	maxCollisionRetries = 5
)

func shortKeyFromURL(url string) string {
	sum := sha256.Sum256([]byte(url))
	n := new(big.Int).SetBytes(sum[:])
	base := big.NewInt(int64(len(base62Alphabet)))
	mod := new(big.Int)

	var b strings.Builder
	b.Grow(shortKeyLength)
	for b.Len() < shortKeyLength {
		n.DivMod(n, base, mod)
		b.WriteByte(base62Alphabet[mod.Int64()])
	}
	return b.String()
}

type urlService struct {
	urlRepo     repository.URLRepository
	requestRepo repository.RequestRepository
}

func NewURLService(ur repository.URLRepository, rr repository.RequestRepository) URLService {
	return &urlService{urlRepo: ur, requestRepo: rr}
}

func (s *urlService) ShortenURL(ctx context.Context, originalURL, userAgent string) (*model.URL, error) {
	shortKey := shortKeyFromURL(originalURL)

	for attempt := 0; attempt <= maxCollisionRetries; attempt++ {
		existing, err := s.urlRepo.GetByShortKey(ctx, shortKey)
		if err == nil {
			if existing.URL == originalURL {
				if _, logErr := s.requestRepo.LogCreateRequest(ctx, existing.ID, userAgent); logErr != nil {
					slog.WarnContext(ctx, "ShortenURL: log create request failed for existing url",
						slog.String("url_id", existing.ID.String()),
						slog.String("err", logErr.Error()),
					)
				}
				return existing, nil
			}
			slog.WarnContext(ctx, "ShortenURL: short key collision, salting and retrying",
				slog.String("short_key", shortKey),
				slog.Int("attempt", attempt+1),
			)
			shortKey = shortKeyFromURL(fmt.Sprintf("%s:%d", originalURL, attempt+1))
			continue
		}
		if errors.Is(err, repository.ErrNotFound) {
			break
		}
		slog.ErrorContext(ctx, "ShortenURL: lookup failed",
			slog.String("url", originalURL),
			slog.String("short_key", shortKey),
			slog.String("err", err.Error()),
		)
		return nil, fmt.Errorf("lookup short key: %w", err)
	}

	url := model.NewURL(originalURL, shortKey)
	if err := s.urlRepo.Create(ctx, &url); err != nil {
		slog.ErrorContext(ctx, "ShortenURL: create failed",
			slog.String("url", originalURL),
			slog.String("short_key", shortKey),
			slog.String("err", err.Error()),
		)
		return nil, fmt.Errorf("create url: %w", err)
	}

	if _, err := s.requestRepo.LogCreateRequest(ctx, url.ID, userAgent); err != nil {
		slog.WarnContext(ctx, "ShortenURL: log create request failed for new url",
			slog.String("url_id", url.ID.String()),
			slog.String("err", err.Error()),
		)
	}

	return &url, nil
}

func (s *urlService) Redirect(ctx context.Context, key string) (string, error) {
	url, err := s.urlRepo.GetByShortKey(ctx, key)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			slog.ErrorContext(ctx, "Redirect: lookup failed",
				slog.String("short_key", key),
				slog.String("err", err.Error()),
			)
		}
		return "", err
	}

	// Detached from the request context (which is canceled the moment the
	// 301 is written) but still bounded — a hung DB shouldn't leak this
	// goroutine.
	go func(id string) {
		bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.urlRepo.UpdateLastUsed(bgCtx, id); err != nil {
			slog.Error("Redirect: update last_used_at failed (async)",
				slog.String("url_id", id),
				slog.String("err", err.Error()),
			)
		}
	}(url.ID.String())

	return url.URL, nil
}
