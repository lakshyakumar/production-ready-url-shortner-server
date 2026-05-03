package model

import "github.com/google/uuid"

type IncomingRequest struct {
	Base
	URLID     uuid.UUID `json:"url_id" db:"url_id"`
	IPAddress string    `json:"ip_address" db:"ip_address"`
	UserAgent string    `json:"user_agent" db:"user_agent"`
}
