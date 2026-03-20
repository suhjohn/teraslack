package service

import (
	"context"
	"fmt"

	"github.com/suhjohn/workspace/internal/domain"
	"github.com/suhjohn/workspace/internal/repository"
)

// SearchService contains business logic for search operations.
// Uses Postgres full-text search as a baseline. ClickHouse and Turbopuffer
// integrations extend this with analytics and semantic search.
type SearchService struct {
	msgRepo      repository.MessageRepository
	fileRepo     repository.FileRepository
	clickhouse   ClickHouseClient
	turbopuffer  TurbopufferClient
}

// ClickHouseClient defines the interface for ClickHouse search operations.
type ClickHouseClient interface {
	SearchMessages(ctx context.Context, teamID, query string, limit int) ([]domain.Message, error)
	SearchFiles(ctx context.Context, teamID, query string, limit int) ([]domain.File, error)
	IndexMessage(ctx context.Context, msg *domain.Message) error
}

// TurbopufferClient defines the interface for vector search operations.
type TurbopufferClient interface {
	Upsert(ctx context.Context, id string, embedding []float32, metadata map[string]string) error
	Query(ctx context.Context, embedding []float32, limit int, filters map[string]string) ([]VectorResult, error)
	GetEmbedding(ctx context.Context, text string) ([]float32, error)
}

// VectorResult represents a single vector search result.
type VectorResult struct {
	ID       string
	Score    float64
	Metadata map[string]string
}

// NewSearchService creates a new SearchService.
func NewSearchService(
	msgRepo repository.MessageRepository,
	fileRepo repository.FileRepository,
	clickhouse ClickHouseClient,
	turbopuffer TurbopufferClient,
) *SearchService {
	return &SearchService{
		msgRepo:     msgRepo,
		fileRepo:    fileRepo,
		clickhouse:  clickhouse,
		turbopuffer: turbopuffer,
	}
}

// SearchMessages searches messages using ClickHouse if available, falls back to listing.
func (s *SearchService) SearchMessages(ctx context.Context, params domain.SearchMessagesParams) (*domain.CursorPage[domain.Message], error) {
	if params.Query == "" {
		return nil, fmt.Errorf("query: %w", domain.ErrInvalidArgument)
	}

	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	if s.clickhouse != nil {
		msgs, err := s.clickhouse.SearchMessages(ctx, params.TeamID, params.Query, limit)
		if err != nil {
			return nil, fmt.Errorf("clickhouse search: %w", err)
		}
		return &domain.CursorPage[domain.Message]{
			Items:   msgs,
			HasMore: len(msgs) >= limit,
		}, nil
	}

	// Fallback: no ClickHouse configured - return empty results with a message
	return &domain.CursorPage[domain.Message]{
		Items: []domain.Message{},
	}, nil
}

// SearchFiles searches files using ClickHouse if available.
func (s *SearchService) SearchFiles(ctx context.Context, params domain.SearchFilesParams) (*domain.CursorPage[domain.File], error) {
	if params.Query == "" {
		return nil, fmt.Errorf("query: %w", domain.ErrInvalidArgument)
	}

	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	if s.clickhouse != nil {
		files, err := s.clickhouse.SearchFiles(ctx, params.TeamID, params.Query, limit)
		if err != nil {
			return nil, fmt.Errorf("clickhouse search files: %w", err)
		}
		return &domain.CursorPage[domain.File]{
			Items:   files,
			HasMore: len(files) >= limit,
		}, nil
	}

	return &domain.CursorPage[domain.File]{
		Items: []domain.File{},
	}, nil
}

// SemanticSearch performs vector similarity search using Turbopuffer.
func (s *SearchService) SemanticSearch(ctx context.Context, params domain.SemanticSearchParams) ([]domain.SemanticSearchResult, error) {
	if params.Query == "" {
		return nil, fmt.Errorf("query: %w", domain.ErrInvalidArgument)
	}

	limit := params.Limit
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	if s.turbopuffer == nil {
		return []domain.SemanticSearchResult{}, nil
	}

	// Get embedding for query text
	embedding, err := s.turbopuffer.GetEmbedding(ctx, params.Query)
	if err != nil {
		return nil, fmt.Errorf("get embedding: %w", err)
	}

	filters := map[string]string{
		"team_id": params.TeamID,
	}
	if params.ChannelID != "" {
		filters["channel_id"] = params.ChannelID
	}

	results, err := s.turbopuffer.Query(ctx, embedding, limit, filters)
	if err != nil {
		return nil, fmt.Errorf("vector query: %w", err)
	}

	// Hydrate results with full messages
	var searchResults []domain.SemanticSearchResult
	for _, r := range results {
		channelID := r.Metadata["channel_id"]
		ts := r.Metadata["ts"]
		if channelID == "" || ts == "" {
			continue
		}

		msg, err := s.msgRepo.Get(ctx, channelID, ts)
		if err != nil {
			continue // Skip if message was deleted
		}

		searchResults = append(searchResults, domain.SemanticSearchResult{
			Message: *msg,
			Score:   r.Score,
		})
	}

	if searchResults == nil {
		searchResults = []domain.SemanticSearchResult{}
	}
	return searchResults, nil
}

// IndexMessage indexes a message for both ClickHouse and Turbopuffer.
func (s *SearchService) IndexMessage(ctx context.Context, msg *domain.Message) error {
	if s.clickhouse != nil {
		if err := s.clickhouse.IndexMessage(ctx, msg); err != nil {
			return fmt.Errorf("clickhouse index: %w", err)
		}
	}

	if s.turbopuffer != nil && msg.Text != "" {
		embedding, err := s.turbopuffer.GetEmbedding(ctx, msg.Text)
		if err != nil {
			return fmt.Errorf("get embedding: %w", err)
		}

		metadata := map[string]string{
			"channel_id": msg.ChannelID,
			"ts":         msg.TS,
			"user_id":    msg.UserID,
		}

		id := fmt.Sprintf("%s:%s", msg.ChannelID, msg.TS)
		if err := s.turbopuffer.Upsert(ctx, id, embedding, metadata); err != nil {
			return fmt.Errorf("turbopuffer upsert: %w", err)
		}
	}

	return nil
}
