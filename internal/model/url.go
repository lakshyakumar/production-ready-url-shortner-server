package model

import "time"

type URL struct {
	Base
	URL        string    `json:"url" db:"url"`
	ShortKey   string    `json:"short_key" db:"short_key"`
	LastUsedAt time.Time `json:"last_used" db:"last_used"`
	IsActive   bool      `json:"is_active" db:"is_active"`
}

// NewURL is a constructor to ensure defaults are set correctly
func NewURL(url string, key string) URL {
	return URL{
		Base:     NewBase(),
		URL:      url,
		ShortKey: key,
		IsActive: true, // Logic-level default
	}
}
