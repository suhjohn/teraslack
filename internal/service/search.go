package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/suhjohn/teraslack/internal/domain"
)

// SearchService contains business logic for unified search across all resource types.
// Uses Turbopuffer as the single search index backing all resource types.
type SearchService struct {
	turbopuffer TurbopufferClient
}

// TurbopufferClient defines the interface for vector search operations.
// A single Turbopuffer namespace indexes all resource types (users, messages,
// conversations, files, etc.) with a "type" attribute for filtering.
type TurbopufferClient interface {
	// Upsert inserts or updates a document in the search index.
	Upsert(ctx context.Context, id string, embedding []float32, metadata map[string]any) error
	// Query performs a vector similarity search with optional attribute filters.
	Query(ctx context.Context, embedding []float32, limit int, filters map[string]any) ([]VectorResult, error)
	// GetEmbedding generates an embedding vector for the given text.
	GetEmbedding(ctx context.Context, text string) ([]float32, error)
}

// VectorResult represents a single vector search result from Turbopuffer.
type VectorResult struct {
	ID       string
	Score    float64
	Metadata map[string]any
}

// NewSearchService creates a new SearchService.
func NewSearchService(turbopuffer TurbopufferClient) *SearchService {
	return &SearchService{
		turbopuffer: turbopuffer,
	}
}

// Search performs a unified search across all resource types using Turbopuffer.
// Results are ranked by relevance and optionally filtered by type.
func (s *SearchService) Search(ctx context.Context, params domain.SearchParams) ([]domain.SearchResult, error) {
	if params.Query == "" {
		return nil, fmt.Errorf("query: %w", domain.ErrInvalidArgument)
	}

	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	if s.turbopuffer == nil {
		return []domain.SearchResult{}, nil
	}

	// Get embedding for query text
	embedding, err := s.turbopuffer.GetEmbedding(ctx, params.Query)
	if err != nil {
		return nil, fmt.Errorf("get embedding: %w", err)
	}

	// Build filters
	filters := map[string]any{
		"team_id": params.TeamID,
	}
	if len(params.Types) > 0 {
		filters["type"] = params.Types
	}

	results, err := s.turbopuffer.Query(ctx, embedding, limit, filters)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}

	// Convert to domain search results
	searchResults := make([]domain.SearchResult, 0, len(results))
	for _, r := range results {
		resultType, _ := r.Metadata["type"].(string)
		data, _ := r.Metadata["data"]

		dataJSON, err := json.Marshal(data)
		if err != nil {
			continue
		}

		searchResults = append(searchResults, domain.SearchResult{
			Type:  resultType,
			Score: r.Score,
			Data:  dataJSON,
		})
	}

	return searchResults, nil
}

// Index indexes any resource into the unified Turbopuffer search index.
func (s *SearchService) Index(ctx context.Context, resourceType, id, teamID, content string, data any) error {
	if s.turbopuffer == nil {
		return nil
	}

	if content == "" {
		return nil
	}

	embedding, err := s.turbopuffer.GetEmbedding(ctx, content)
	if err != nil {
		return fmt.Errorf("get embedding: %w", err)
	}

	metadata := map[string]any{
		"type":    resourceType,
		"team_id": teamID,
		"data":    data,
	}

	if err := s.turbopuffer.Upsert(ctx, fmt.Sprintf("%s:%s", resourceType, id), embedding, metadata); err != nil {
		return fmt.Errorf("turbopuffer upsert: %w", err)
	}

	return nil
}
