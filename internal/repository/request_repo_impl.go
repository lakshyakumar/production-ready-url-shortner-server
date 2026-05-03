package repository

import (
	"context"
	"log/slog"

	contextutil "github/lakshyakumar/production-ready-url-shortner-server/internal/contextUtil"
	"github/lakshyakumar/production-ready-url-shortner-server/internal/model"

	"github.com/google/uuid"
)

type requestRepoImpl struct {
	router Router
}

func NewRequestRepository(r Router) RequestRepository {
	return &requestRepoImpl{router: r}
}

func (r *requestRepoImpl) LogCreateRequest(ctx context.Context, urlId uuid.UUID, userAgent string) (*model.IncomingRequest, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	ip := contextutil.GetClientIP(ctx)

	req := &model.IncomingRequest{
		Base:      model.NewBase(),
		URLID:     urlId,
		IPAddress: ip,
		UserAgent: userAgent,
	}

	query := `
		INSERT INTO incoming_requests (id, created_at, updated_at, url_id, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := r.router.Writer().Exec(ctx, query,
		req.ID,
		req.CreatedAt,
		req.UpdatedAt,
		req.URLID,
		req.IPAddress,
		req.UserAgent,
	)
	if err != nil {
		slog.ErrorContext(ctx, "incoming_requests.LogCreateRequest failed",
			slog.String("id", req.ID.String()),
			slog.String("url_id", urlId.String()),
			slog.String("err", err.Error()),
		)
		return nil, err
	}

	return req, nil
}
