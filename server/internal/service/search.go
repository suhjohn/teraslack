package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

// SearchService contains business logic for unified search across all resource types.
// Uses Turbopuffer as the single search index backing all resource types.
// Documents are sharded across 256 namespaces based on the first 2 hex chars
// of the UUID portion of entity IDs (e.g., "U_01abc..." → namespace "01").
type SearchService struct {
	turbopuffer     TurbopufferClient
	externalMembers repository.ExternalMemberRepository
	convRepo        repository.ConversationRepository
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

func (s *SearchService) SetExternalMemberRepository(repo repository.ExternalMemberRepository) {
	s.externalMembers = repo
}

func (s *SearchService) SetConversationRepository(repo repository.ConversationRepository) {
	s.convRepo = repo
}

// Search performs a unified search across all resource types using Turbopuffer.
// Fans out queries across all 256 namespaces in parallel, merges and sorts
// results by score, and returns the top-k.
func (s *SearchService) Search(ctx context.Context, params domain.SearchParams) ([]domain.SearchResult, error) {
	if params.Query == "" {
		return nil, fmt.Errorf("query: %w", domain.ErrInvalidArgument)
	}
	workspaceID, visibility, err := s.resolveSearchWorkspace(ctx, params.WorkspaceID)
	if err != nil {
		return nil, err
	}
	params.WorkspaceID = workspaceID

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
		"workspace_id": params.WorkspaceID,
	}
	if len(params.Types) > 0 {
		filters["type"] = params.Types
	}
	queryLimit := limit
	if visibility.external {
		queryLimit = limit * 4
		if queryLimit < 50 {
			queryLimit = 50
		}
		if queryLimit > 100 {
			queryLimit = 100
		}
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

			results, qErr := s.turbopuffer.Query(ctx, namespace, embedding, queryLimit, filters)
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

	if visibility.external {
		allResults = filterSearchResultsByVisibility(allResults, visibility)
	}

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

type searchVisibilityScope struct {
	external             bool
	allowedConversations map[string]struct{}
}

func (s *SearchService) resolveSearchWorkspace(ctx context.Context, requested string) (string, searchVisibilityScope, error) {
	requested = strings.TrimSpace(requested)
	ctxWorkspace := ctxutil.GetWorkspaceID(ctx)
	if requested == "" || ctxWorkspace == "" || requested == ctxWorkspace {
		workspaceID, err := resolveWorkspaceID(ctx, requested)
		if err != nil {
			return "", searchVisibilityScope{}, err
		}
		scope, err := s.searchVisibilityScope(ctx, workspaceID)
		if err != nil {
			return "", searchVisibilityScope{}, err
		}
		return workspaceID, scope, nil
	}
	scope, err := s.searchVisibilityScope(ctx, requested)
	if err != nil {
		return "", searchVisibilityScope{}, err
	}
	if !scope.external {
		return "", searchVisibilityScope{}, domain.ErrForbidden
	}
	return requested, scope, nil
}

func (s *SearchService) searchVisibilityScope(ctx context.Context, workspaceID string) (searchVisibilityScope, error) {
	if actorAccountID(ctx) == "" && hasWorkspaceUserContext(ctx, workspaceID) {
		return searchVisibilityScope{}, nil
	}
	conversationIDs, err := s.listVisibleConversationIDs(ctx, workspaceID)
	if err != nil {
		return searchVisibilityScope{}, err
	}
	if len(conversationIDs) > 0 {
		return newSearchVisibilityScope(conversationIDs), nil
	}
	return searchVisibilityScope{}, nil
}

func (s *SearchService) listVisibleConversationIDs(ctx context.Context, workspaceID string) ([]string, error) {
	accountID := actorAccountID(ctx)
	if s.convRepo == nil || workspaceID == "" || accountID == "" {
		return nil, nil
	}

	cursor := ""
	allowed := make(map[string]struct{})
	for {
		page, err := s.convRepo.List(ctx, domain.ListConversationsParams{
			WorkspaceID:     workspaceID,
			AccountID:       accountID,
			ExcludeArchived: true,
			Cursor:          cursor,
			Limit:           100,
		})
		if err != nil {
			return nil, err
		}
		for _, conv := range page.Items {
			if conversationWorkspaceID(&conv) != workspaceID {
				continue
			}
			allowed[conv.ID] = struct{}{}
		}
		if !page.HasMore || page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}

	conversationIDs := make([]string, 0, len(allowed))
	for id := range allowed {
		conversationIDs = append(conversationIDs, id)
	}
	sort.Strings(conversationIDs)
	return conversationIDs, nil
}

func newSearchVisibilityScope(conversationIDs []string) searchVisibilityScope {
	allowed := make(map[string]struct{}, len(conversationIDs))
	for _, id := range conversationIDs {
		if id == "" {
			continue
		}
		allowed[id] = struct{}{}
	}
	return searchVisibilityScope{
		external:             len(allowed) > 0,
		allowedConversations: allowed,
	}
}

func filterSearchResultsByVisibility(results []VectorResult, scope searchVisibilityScope) []VectorResult {
	if !scope.external || len(scope.allowedConversations) == 0 {
		return results
	}
	filtered := make([]VectorResult, 0, len(results))
	for _, result := range results {
		if searchResultVisible(result, scope.allowedConversations) {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

func searchResultVisible(result VectorResult, allowedConversations map[string]struct{}) bool {
	resultType, _ := result.Metadata["type"].(string)
	switch resultType {
	case "conversation":
		id := searchResultField(result.Metadata, "conversation_id", "id")
		_, ok := allowedConversations[id]
		return ok
	case "message":
		id := searchResultField(result.Metadata, "channel_id", "conversation_id")
		_, ok := allowedConversations[id]
		return ok
	case "file":
		for _, id := range searchResultFields(result.Metadata, "channel_ids", "channels") {
			if _, ok := allowedConversations[id]; ok {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func searchResultField(metadata map[string]any, fields ...string) string {
	for _, field := range fields {
		if value, ok := searchDataField(metadata, field); ok {
			return value
		}
	}
	return ""
}

func searchResultFields(metadata map[string]any, fields ...string) []string {
	for _, field := range fields {
		if values, ok := searchDataFields(metadata, field); ok && len(values) > 0 {
			return values
		}
	}
	return nil
}

func searchDataField(metadata map[string]any, field string) (string, bool) {
	if value, ok := metadata[field].(string); ok && value != "" {
		return value, true
	}
	dataMap, ok := searchResultDataMap(metadata)
	if !ok {
		return "", false
	}
	value, ok := dataMap[field].(string)
	return value, ok && value != ""
}

func searchDataFields(metadata map[string]any, field string) ([]string, bool) {
	if raw, ok := metadata[field]; ok {
		if values := anyToStringSlice(raw); len(values) > 0 {
			return values, true
		}
	}
	dataMap, ok := searchResultDataMap(metadata)
	if !ok {
		return nil, false
	}
	values := anyToStringSlice(dataMap[field])
	return values, len(values) > 0
}

func searchResultDataMap(metadata map[string]any) (map[string]any, bool) {
	raw, ok := metadata["data"]
	if !ok || raw == nil {
		return nil, false
	}
	switch v := raw.(type) {
	case map[string]any:
		return v, true
	case json.RawMessage:
		var out map[string]any
		if err := json.Unmarshal(v, &out); err == nil {
			return out, true
		}
	case []byte:
		var out map[string]any
		if err := json.Unmarshal(v, &out); err == nil {
			return out, true
		}
	case string:
		var out map[string]any
		if err := json.Unmarshal([]byte(v), &out); err == nil {
			return out, true
		}
	}
	return nil, false
}

func anyToStringSlice(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
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
func (s *SearchService) Index(ctx context.Context, resourceType, id, workspaceID, content string, data any) error {
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
		"type":         resourceType,
		"workspace_id": workspaceID,
		"data":         data,
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
