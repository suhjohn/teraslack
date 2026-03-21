package service

import (
	"context"
	"testing"

	"github.com/suhjohn/teraslack/internal/domain"
)

func TestSearchService_Search_EmptyQuery(t *testing.T) {
	svc := NewSearchService(nil)

	_, err := svc.Search(context.Background(), domain.SearchParams{
		Query: "",
	})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestSearchService_Search_NoTurbopuffer(t *testing.T) {
	svc := NewSearchService(nil)

	results, err := svc.Search(context.Background(), domain.SearchParams{
		TeamID: "T123",
		Query:  "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchService_Search_WithTypes(t *testing.T) {
	svc := NewSearchService(nil)

	// With type filter, still returns empty when no Turbopuffer configured
	results, err := svc.Search(context.Background(), domain.SearchParams{
		TeamID: "T123",
		Query:  "alice",
		Types:  []string{"user", "conversation"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchService_Index_NoTurbopuffer(t *testing.T) {
	svc := NewSearchService(nil)

	// Index should be a no-op when Turbopuffer is not configured
	err := svc.Index(context.Background(), "user", "U123", "T123", "alice", map[string]string{"name": "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearchService_Index_EmptyContent(t *testing.T) {
	svc := NewSearchService(nil)

	// Empty content should be a no-op
	err := svc.Index(context.Background(), "user", "U123", "T123", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
