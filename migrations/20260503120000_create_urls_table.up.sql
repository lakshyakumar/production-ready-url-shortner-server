CREATE TABLE IF NOT EXISTS urls (
    id            UUID PRIMARY KEY,
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,
    deleted_at    TIMESTAMPTZ,
    original_url  TEXT NOT NULL,
    short_key     VARCHAR(11) NOT NULL,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    last_used_at  TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS urls_short_key_uidx
    ON urls (short_key)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS urls_last_used_at_idx
    ON urls (last_used_at)
    WHERE deleted_at IS NULL;
