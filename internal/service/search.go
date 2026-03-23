package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

// SearchService contains business logic for unified search across all resource types.
// Uses Turbopuffer as the single search index backing all resource types.
// Documents are sharded across 256 namespaces based on the first 2 hex chars
// of the UUID portion of entity IDs (e.g., "U_01abc..." → namespace "01").
type SearchService struct {
	turbopuffer    TurbopufferClient
	externalAccess repository.ExternalPrincipalAccessRepository
}

// TurbopufferClient defines the interface for vector search operations.
// Each operation targets a specific namespace. Namespaces are determined
// by the first 2 hex characters of the entity's UUID portion, giving
// 256 namespaces for uniform write distribution.
type TurbopufferClient interface {
	// Upsert inserts or updates a document in the given namespace.
	Upsert(ctx context.Context, namespace string, id string, embedding []float32, metadata map[string]any) error
	// Delete removes a document from the given namespace.
	Delete(ctx context.Context, namespace string, id string) error
	// Query performs a vector similarity search within a single namespace.
	Query(ctx context.Context, namespace string, embedding []float32, limit int, filters map[string]any) ([]VectorResult, error)
	// GetEmbedding generates an embedding vector for the given text.
	GetEmbedding(ctx context.Context, text string) ([]float32, error)
}

// VectorResult represents a single vector search result from Turbopuffer.
type VectorResult struct {
	ID       string
	Score    float64
	Metadata map[string]any
}

// NamespaceFromID extracts the Turbopuffer namespace from an entity ID.
// IDs are formatted as "{prefix}_{uuidv7}" (e.g., "U_0192d4a8-7e1b-...").
// The namespace is the first 2 hex characters of the UUID portion → "01".
// For composite IDs like "C_abc123:1234567890.123456", uses the first part.
func NamespaceFromID(id string) string {
	idx := strings.Index(id, "_")
	if idx < 0 || idx+3 > len(id) {
		if len(id) >= 2 {
			return strings.ToLower(id[:2])
		}
		return "00"
	}
	return strings.ToLower(id[idx+1 : idx+3])
}

// MessageSearchID returns the canonical search document ID for a message.
func MessageSearchID(channelID, ts string) string {
	return fmt.Sprintf("%s:%s", channelID, ts)
}

// AllNamespaces returns all 256 possible namespace prefixes (00-ff).
func AllNamespaces() []string {
	ns := make([]string, 256)
	for i := 0; i < 256; i++ {
		ns[i] = fmt.Sprintf("%02x", i)
	}
	return ns
}

// NewSearchService creates a new SearchService.
func NewSearchService(turbopuffer TurbopufferClient) *SearchService {
	return &SearchService{
		turbopuffer: turbopuffer,
	}
}

func (s *SearchService) SetExternalAccessRepository(repo repository.ExternalPrincipalAccessRepository) {
	s.externalAccess = repo
}

// Search performs a unified search across all resource types using Turbopuffer.
// Fans out queries across all 256 namespaces in parallel, merges and sorts
// results by score, and returns the top-k.
func (s *SearchService) Search(ctx context.Context, params domain.SearchParams) ([]domain.SearchResult, error) {
	if params.Query == "" {
		return nil, fmt.Errorf("query: %w", domain.ErrInvalidArgument)
	}
	teamID, err := resolveTeamID(ctx, params.TeamID)
	if err != nil {
		return nil, err
	}
	params.TeamID = teamID
	if external, err := isExternalSharedActor(ctx, s.externalAccess); err != nil {
		return nil, err
	} else if external {
		return nil, domain.ErrForbidden
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

	// Fan out across all 256 namespaces in parallel
	namespaces := AllNamespaces()
	type nsResult struct {
		results []VectorResult
		err     error
	}

	resultsCh := make(chan nsResult, len(namespaces))
	var wg sync.WaitGroup

	// Semaphore to limit concurrent queries
	sem := make(chan struct{}, 32)

	for _, ns := range namespaces {
		wg.Add(1)
		go func(namespace string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			results, qErr := s.turbopuffer.Query(ctx, namespace, embedding, limit, filters)
			resultsCh <- nsResult{results: results, err: qErr}
		}(ns)
	}

	wg.Wait()
	close(resultsCh)

	// Merge results from all namespaces
	var allResults []VectorResult
	for nr := range resultsCh {
		if nr.err != nil {
			continue // partial results are better than none
		}
		allResults = append(allResults, nr.results...)
	}

	// Sort by score descending
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})

	// Take top-k
	if len(allResults) > limit {
		allResults = allResults[:limit]
	}

	// Convert to domain search results
	searchResults := make([]domain.SearchResult, 0, len(allResults))
	for _, r := range allResults {
		resultType, _ := r.Metadata["type"].(string)
		dataJSON, err := normalizeSearchData(r.Metadata["data"])
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

func normalizeSearchData(data any) (json.RawMessage, error) {
	switch v := data.(type) {
	case nil:
		return json.RawMessage("null"), nil
	case json.RawMessage:
		out := make(json.RawMessage, len(v))
		copy(out, v)
		return out, nil
	case []byte:
		if json.Valid(v) {
			out := make(json.RawMessage, len(v))
			copy(out, v)
			return out, nil
		}
		return json.Marshal(string(v))
	case string:
		if json.Valid([]byte(v)) {
			out := make(json.RawMessage, len(v))
			copy(out, v)
			return out, nil
		}
		return json.Marshal(v)
	default:
		return json.Marshal(v)
	}
}

// Index indexes any resource into the Turbopuffer search index.
// The namespace is derived from the entity ID's first 2 hex chars of the UUID portion.
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

	namespace := NamespaceFromID(id)

	metadata := map[string]any{
		"type":    resourceType,
		"team_id": teamID,
		"data":    data,
	}

	tpID := fmt.Sprintf("%s:%s", resourceType, id)
	if err := s.turbopuffer.Upsert(ctx, namespace, tpID, embedding, metadata); err != nil {
		return fmt.Errorf("turbopuffer upsert: %w", err)
	}

	return nil
}

// DeleteFromIndex removes a resource from the Turbopuffer search index.
func (s *SearchService) DeleteFromIndex(ctx context.Context, resourceType, id string) error {
	if s.turbopuffer == nil {
		return nil
	}

	namespace := NamespaceFromID(id)
	tpID := fmt.Sprintf("%s:%s", resourceType, id)
	if err := s.turbopuffer.Delete(ctx, namespace, tpID); err != nil {
		return fmt.Errorf("turbopuffer delete: %w", err)
	}

	return nil
}
