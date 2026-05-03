package model_test

import (
	"testing"
	"time"

	"github/lakshyakumar/production-ready-url-shortner-server/internal/model"
)

func TestNewBase_PopulatesIDAndTimestamps(t *testing.T) {
	t.Parallel()
	before := time.Now().UTC()
	b := model.NewBase()
	after := time.Now().UTC()

	if b.ID.String() == "" {
		t.Error("ID is empty")
	}
	if b.CreatedAt.Before(before) || b.CreatedAt.After(after) {
		t.Errorf("CreatedAt %v not within [%v, %v]", b.CreatedAt, before, after)
	}
	if b.UpdatedAt != b.CreatedAt {
		t.Errorf("UpdatedAt should equal CreatedAt at construction: got %v vs %v", b.UpdatedAt, b.CreatedAt)
	}
	if b.DeletedAt != nil {
		t.Errorf("DeletedAt should be nil for fresh records, got %v", *b.DeletedAt)
	}
}

func TestNewBase_IDsAreUnique(t *testing.T) {
	t.Parallel()
	a := model.NewBase()
	b := model.NewBase()
	if a.ID == b.ID {
		t.Error("two NewBase calls produced the same UUID")
	}
}

func TestNewURL_DefaultsActiveAndCarriesArgs(t *testing.T) {
	t.Parallel()
	u := model.NewURL("https://example.com", "abcdefghijk")

	if u.URL != "https://example.com" {
		t.Errorf("URL: got %q want %q", u.URL, "https://example.com")
	}
	if u.ShortKey != "abcdefghijk" {
		t.Errorf("ShortKey: got %q want %q", u.ShortKey, "abcdefghijk")
	}
	if !u.IsActive {
		t.Error("IsActive should default to true")
	}
	if u.ID.String() == "" {
		t.Error("ID is empty (Base not initialized)")
	}
}
