package service

import (
	"context"
	"testing"

	"github.com/suhjohn/workspace/internal/domain"
)

func TestSearchService_SearchMessages_NoClickHouse(t *testing.T) {
	svc := NewSearchService(nil, nil, nil, nil)

	// Empty query should error
	_, err := svc.SearchMessages(context.Background(), domain.SearchMessagesParams{
		Query: "",
	})
	if err == nil {
		t.Fatal("expected error for empty query")
	}

	// Valid query with no ClickHouse returns empty
	page, err := svc.SearchMessages(context.Background(), domain.SearchMessagesParams{
		TeamID: "T123",
		Query:  "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(page.Items))
	}
}

func TestSearchService_SearchFiles_NoClickHouse(t *testing.T) {
	svc := NewSearchService(nil, nil, nil, nil)

	_, err := svc.SearchFiles(context.Background(), domain.SearchFilesParams{
		Query: "",
	})
	if err == nil {
		t.Fatal("expected error for empty query")
	}

	page, err := svc.SearchFiles(context.Background(), domain.SearchFilesParams{
		TeamID: "T123",
		Query:  "report",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(page.Items))
	}
}

func TestSearchService_SemanticSearch_NoTurbopuffer(t *testing.T) {
	svc := NewSearchService(nil, nil, nil, nil)

	_, err := svc.SemanticSearch(context.Background(), domain.SemanticSearchParams{
		Query: "",
	})
	if err == nil {
		t.Fatal("expected error for empty query")
	}

	results, err := svc.SemanticSearch(context.Background(), domain.SemanticSearchParams{
		TeamID: "T123",
		Query:  "how to deploy",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}
