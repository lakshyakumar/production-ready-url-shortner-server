CREATE TABLE IF NOT EXISTS incoming_requests (
    id          UUID PRIMARY KEY,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    deleted_at  TIMESTAMPTZ,
    url_id      UUID NOT NULL REFERENCES urls(id) ON DELETE CASCADE,
    ip_address  VARCHAR(45) NOT NULL,
    user_agent  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS incoming_requests_url_id_idx
    ON incoming_requests (url_id);

CREATE INDEX IF NOT EXISTS incoming_requests_created_at_idx
    ON incoming_requests (created_at);
