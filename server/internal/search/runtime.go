package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	tpuf "github.com/turbopuffer/turbopuffer-go"
	"github.com/turbopuffer/turbopuffer-go/option"
	"golang.org/x/sync/errgroup"

	"github.com/johnsuh/teraslack/server/internal/api"
	"github.com/johnsuh/teraslack/server/internal/config"
	"github.com/johnsuh/teraslack/server/internal/dbsqlc"
	"github.com/johnsuh/teraslack/server/internal/domain"
	"github.com/johnsuh/teraslack/server/internal/embedding"
)

func NewRuntime(cfg config.Config, db *pgxpool.Pool, logger *slog.Logger) *Runtime {
	if logger == nil {
		logger = slog.Default()
	}
	clientOptions := []option.RequestOption{}
	if strings.TrimSpace(cfg.TurbopufferAPIKey) != "" {
		clientOptions = append(clientOptions, option.WithAPIKey(strings.TrimSpace(cfg.TurbopufferAPIKey)))
	}
	if strings.TrimSpace(cfg.TurbopufferRegion) != "" {
		clientOptions = append(clientOptions, option.WithRegion(strings.TrimSpace(cfg.TurbopufferRegion)))
	}
	clientOptions = append(clientOptions, option.WithMaxRetries(2))
	return &Runtime{
		cfg:             cfg,
		db:              db,
		queries:         dbsqlc.New(db),
		logger:          logger,
		tpuf:            tpuf.NewClient(clientOptions...),
		embedder:        embedding.New(cfg.ModalEmbeddingServerURL, cfg.ModalServerAPIKey),
		namespaceVector: map[string]int{},
	}
}

func (r *Runtime) Configured() bool {
	return r != nil &&
		strings.TrimSpace(r.cfg.TurbopufferAPIKey) != "" &&
		strings.TrimSpace(r.cfg.TurbopufferRegion) != "" &&
		strings.TrimSpace(r.cfg.TurbopufferNSPrefix) != ""
}

func (r *Runtime) Search(ctx context.Context, auth domain.AuthContext, request api.SearchRequest) (api.SearchResponse, error) {
	if !r.Configured() {
		return api.SearchResponse{}, ErrNotConfigured
	}

	query := strings.TrimSpace(request.Query)
	if query == "" {
		return api.SearchResponse{}, invalid("query", "required", "query is required.")
	}
	limit := request.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	kinds, err := normalizeKinds(request.Kinds)
	if err != nil {
		return api.SearchResponse{}, err
	}

	workspaceID, err := parseUUID(stringValue(request.WorkspaceID), "workspace_id")
	if err != nil {
		return api.SearchResponse{}, err
	}
	conversationID, err := parseUUID(stringValue(request.ConversationID), "conversation_id")
	if err != nil {
		return api.SearchResponse{}, err
	}

	cursor, err := parseCursor(request.Cursor)
	if err != nil {
		return api.SearchResponse{}, err
	}
	if cursor != nil {
		if cursor.Query != query || !sameStrings(cursor.Kinds, kinds) || cursor.WorkspaceID != stringValue(uuidPtrToStringPtr(workspaceID)) || cursor.ConversationID != stringValue(uuidPtrToStringPtr(conversationID)) {
			return api.SearchResponse{}, invalid("cursor", "invalid_value", "Cursor does not match this search request.")
		}
	}

	allowGlobal := auth.APIKeyWorkspaceID == nil
	if auth.APIKeyWorkspaceID != nil {
		if workspaceID == nil {
			workspaceID = auth.APIKeyWorkspaceID
		} else if *workspaceID != *auth.APIKeyWorkspaceID {
			return api.SearchResponse{}, forbidden("This API key cannot search outside its workspace.")
		}
	}

	if workspaceID != nil {
		visible, err := r.workspaceVisible(ctx, auth.UserID, *workspaceID)
		if err != nil {
			return api.SearchResponse{}, err
		}
		if !visible {
			return api.SearchResponse{}, forbidden("You do not have access to this workspace.")
		}
	}

	if conversationID != nil {
		conversation, err := r.loadConversation(ctx, *conversationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return api.SearchResponse{}, notFound("Conversation not found.")
		}
		if err != nil {
			return api.SearchResponse{}, err
		}
		visible, err := r.conversationVisible(ctx, auth.UserID, allowGlobal, conversation)
		if err != nil {
			return api.SearchResponse{}, err
		}
		if !visible {
			return api.SearchResponse{}, forbidden("You do not have access to this conversation.")
		}
		if workspaceID != nil && (conversation.WorkspaceID == nil || *conversation.WorkspaceID != *workspaceID) {
			return api.SearchResponse{}, invalid("conversation_id", "invalid_value", "Conversation does not belong to the requested workspace.")
		}
	}

	principalIDs, err := r.resolveQueryPrincipals(ctx, auth.UserID, allowGlobal, workspaceID)
	if err != nil {
		return api.SearchResponse{}, err
	}
	if len(principalIDs) == 0 {
		return api.SearchResponse{Items: []api.SearchHit{}}, nil
	}

	var queryEmbedding []float32
	if r.embedder != nil && r.embedder.Configured() {
		embedding, err := r.embedder.EmbedQuery(ctx, query)
		if err != nil {
			r.logger.Warn("query embedding failed; falling back to BM25", "error", err)
		} else {
			queryEmbedding = embedding
		}
	}

	seenKeys := []string{}
	if cursor != nil {
		seenKeys = append(seenKeys, cursor.Seen...)
	}

	candidates, err := r.queryCandidates(ctx, query, queryEmbedding, principalIDs, kinds, workspaceID, conversationID, seenKeys, limit)
	if err != nil {
		return api.SearchResponse{}, err
	}
	if len(candidates) == 0 {
		return api.SearchResponse{Items: []api.SearchHit{}}, nil
	}

	hits := make([]api.SearchHit, 0, limit)
	nextSeen := append([]string(nil), seenKeys...)
	seen := map[string]struct{}{}
	for _, item := range nextSeen {
		seen[item] = struct{}{}
	}
	maxHydrate := len(candidates)
	if maxHydrate > limit*4 {
		maxHydrate = limit * 4
	}
	for _, item := range candidates[:maxHydrate] {
		if len(hits) >= limit {
			break
		}
		hit, ok, err := r.hydrateCandidate(ctx, auth, allowGlobal, item)
		if err != nil {
			return api.SearchResponse{}, err
		}
		if !ok {
			continue
		}
		hits = append(hits, hit)
		if _, ok := seen[item.ResultKey]; !ok {
			seen[item.ResultKey] = struct{}{}
			nextSeen = append(nextSeen, item.ResultKey)
		}
	}

	response := api.SearchResponse{Items: hits}
	if len(candidates) > len(hits) && len(hits) == limit {
		nextCursor, err := encodeCursor(searchCursor{
			Query:          query,
			Kinds:          kinds,
			WorkspaceID:    stringValue(uuidPtrToStringPtr(workspaceID)),
			ConversationID: stringValue(uuidPtrToStringPtr(conversationID)),
			Seen:           nextSeen,
		})
		if err == nil {
			response.NextCursor = nextCursor
		}
	}
	return response, nil
}

func (r *Runtime) queryCandidates(ctx context.Context, query string, queryEmbedding []float32, principalIDs []uuid.UUID, kinds []string, workspaceID *uuid.UUID, conversationID *uuid.UUID, seenKeys []string, limit int) ([]candidate, error) {
	targetLimit := limit * 4
	if targetLimit < 25 {
		targetLimit = 25
	}
	if targetLimit > 100 {
		targetLimit = 100
	}
	corpora := corporaForKinds(kinds)
	aggregated := map[string]candidate{}
	var mu sync.Mutex

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(12)
	for _, corpus := range corpora {
		shards := entityShardCount
		if corpus == contentCorpus {
			shards = contentShardCount
		}
		for shard := 0; shard < shards; shard++ {
			namespace := r.namespaceForSearch(corpus, shard)
			group.Go(func() error {
				bm25Rows, err := r.queryNamespaceBM25(groupCtx, namespace, query, principalIDs, kinds, workspaceID, conversationID, seenKeys, targetLimit)
				if err != nil {
					return err
				}
				vectorRows, err := r.queryNamespaceVector(groupCtx, namespace, queryEmbedding, principalIDs, kinds, workspaceID, conversationID, seenKeys, targetLimit)
				if err != nil {
					return err
				}

				mu.Lock()
				mergeCandidateRows(aggregated, bm25Rows, 1.6)
				mergeCandidateRows(aggregated, vectorRows, 1.0)
				mu.Unlock()
				return nil
			})
		}
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}

	items := make([]candidate, 0, len(aggregated))
	for _, item := range aggregated {
		items = append(items, item)
	}
	sortCandidates(items)
	return items, nil
}

func fmtShard(shard int) string {
	return fmt.Sprintf("%02d", shard)
}

func (r *Runtime) namespaceForSearch(corpus string, shard int) string {
	return fmt.Sprintf("%s-search-%s-v1-%02d", r.cfg.TurbopufferNSPrefix, corpus, shard)
}

func (r *Runtime) buildSearchFilter(principalIDs []uuid.UUID, kinds []string, workspaceID *uuid.UUID, conversationID *uuid.UUID, seenKeys []string) tpuf.Filter {
	filters := make([]tpuf.Filter, 0, 5)
	filters = appendQueryFilter(filters, tpuf.NewFilterContainsAny("read_principal_ids", uuidStrings(principalIDs)))
	if len(kinds) > 0 {
		filters = appendQueryFilter(filters, tpuf.NewFilterIn("kind", kinds))
	}
	if workspaceID != nil {
		filters = appendQueryFilter(filters, tpuf.NewFilterEq("workspace_id", workspaceID.String()))
	}
	if conversationID != nil {
		filters = appendQueryFilter(filters, tpuf.NewFilterEq("conversation_id", conversationID.String()))
	}
	if len(seenKeys) > 0 {
		filters = appendQueryFilter(filters, tpuf.NewFilterNotIn("result_key", seenKeys))
	}
	if len(filters) == 1 {
		return filters[0]
	}
	return tpuf.NewFilterAnd(filters)
}

func (r *Runtime) queryNamespaceBM25(ctx context.Context, namespace string, query string, principalIDs []uuid.UUID, kinds []string, workspaceID *uuid.UUID, conversationID *uuid.UUID, seenKeys []string, limit int) ([]tpuf.Row, error) {
	params := tpuf.NamespaceQueryParams{
		Namespace:         tpuf.String(namespace),
		Consistency:       tpufStrongQueryConsistency(),
		Filters:           r.buildSearchFilter(principalIDs, kinds, workspaceID, conversationID, seenKeys),
		IncludeAttributes: tpufIncludeAttributes("kind", "canonical_id", "result_key", "workspace_id", "conversation_id"),
		Limit:             tpuf.LimitParam{Total: int64(limit)},
		RankBy: tpuf.NewRankByTextSum([]tpuf.RankByText{
			tpuf.NewRankByTextProduct(6, tpuf.NewRankByTextBM25("exact_terms", query)),
			tpuf.NewRankByTextProduct(3, tpuf.NewRankByTextBM25("title", query)),
			tpuf.NewRankByTextBM25("body", query),
		}),
	}
	ns := r.tpuf.Namespace(namespace)
	response, err := ns.Query(ctx, params)
	if err != nil {
		if ignoreNamespaceSearchError(err) {
			return nil, nil
		}
		return nil, err
	}
	return response.Rows, nil
}

func (r *Runtime) queryNamespaceVector(ctx context.Context, namespace string, queryEmbedding []float32, principalIDs []uuid.UUID, kinds []string, workspaceID *uuid.UUID, conversationID *uuid.UUID, seenKeys []string, limit int) ([]tpuf.Row, error) {
	if len(queryEmbedding) == 0 {
		return nil, nil
	}
	params := tpuf.NamespaceQueryParams{
		Namespace:         tpuf.String(namespace),
		Consistency:       tpufStrongQueryConsistency(),
		DistanceMetric:    tpuf.DistanceMetricCosineDistance,
		Filters:           r.buildSearchFilter(principalIDs, kinds, workspaceID, conversationID, seenKeys),
		IncludeAttributes: tpufIncludeAttributes("kind", "canonical_id", "result_key", "workspace_id", "conversation_id"),
		Limit:             tpuf.LimitParam{Total: int64(limit)},
		RankBy:            tpuf.NewRankByVector("vector", queryEmbedding),
	}
	ns := r.tpuf.Namespace(namespace)
	response, err := ns.Query(ctx, params)
	if err != nil {
		if ignoreVectorSearchError(err) {
			return nil, nil
		}
		return nil, err
	}
	return response.Rows, nil
}

func ignoreNamespaceSearchError(err error) bool {
	var apiErr *tpuf.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 202 || apiErr.StatusCode == 404
	}
	return false
}

func ignoreVectorSearchError(err error) bool {
	var apiErr *tpuf.Error
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == 202 || apiErr.StatusCode == 404 {
			return true
		}
		if apiErr.StatusCode == 400 {
			message := strings.ToLower(apiErr.RawJSON())
			return strings.Contains(message, "vector") || strings.Contains(message, "ann")
		}
	}
	return false
}

func mergeCandidateRows(target map[string]candidate, rows []tpuf.Row, weight float64) {
	for rank, row := range rows {
		kind, ok := row["kind"].(string)
		if !ok || strings.TrimSpace(kind) == "" {
			continue
		}
		canonicalID, ok := row["canonical_id"].(string)
		if !ok || strings.TrimSpace(canonicalID) == "" {
			continue
		}
		key, ok := row["result_key"].(string)
		if !ok || strings.TrimSpace(key) == "" {
			key = resultKey(kind, canonicalID)
		}
		item := target[key]
		item.Kind = kind
		item.CanonicalID = canonicalID
		item.ResultKey = key
		item.WorkspaceID = optionalString(row["workspace_id"])
		item.ConversationID = optionalString(row["conversation_id"])
		item.Score += rrfScore(rank, weight)
		target[key] = item
	}
}

func (r *Runtime) hydrateCandidate(ctx context.Context, auth domain.AuthContext, allowGlobal bool, item candidate) (api.SearchHit, bool, error) {
	switch item.Kind {
	case documentKindMessage:
		return r.hydrateMessageHit(ctx, auth, allowGlobal, item)
	case documentKindConversation:
		return r.hydrateConversationHit(ctx, auth, allowGlobal, item)
	case documentKindWorkspace:
		return r.hydrateWorkspaceHit(ctx, auth, item)
	case documentKindUser:
		return r.hydrateUserHit(ctx, auth, item)
	case documentKindEvent:
		return r.hydrateEventHit(ctx, auth, item)
	default:
		return api.SearchHit{}, false, nil
	}
}

func (r *Runtime) hydrateMessageHit(ctx context.Context, auth domain.AuthContext, allowGlobal bool, item candidate) (api.SearchHit, bool, error) {
	messageID, err := uuid.Parse(item.CanonicalID)
	if err != nil {
		return api.SearchHit{}, false, nil
	}
	message, err := r.loadMessage(ctx, messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.SearchHit{}, false, nil
	}
	if err != nil {
		return api.SearchHit{}, false, err
	}
	if message.DeletedAt != nil {
		return api.SearchHit{}, false, nil
	}
	conversation, err := r.loadConversation(ctx, message.ConversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.SearchHit{}, false, nil
	}
	if err != nil {
		return api.SearchHit{}, false, err
	}
	visible, err := r.conversationVisible(ctx, auth.UserID, allowGlobal, conversation)
	if err != nil {
		return api.SearchHit{}, false, err
	}
	if !visible {
		return api.SearchHit{}, false, nil
	}
	title := r.conversationTitle(ctx, conversation)
	apiMessage := messageToAPI(message)
	return api.SearchHit{
		Kind:           documentKindMessage,
		ResourceID:     item.CanonicalID,
		Score:          item.Score,
		Title:          title,
		Snippet:        stringPtr(previewText(messageText(message), 220)),
		WorkspaceID:    uuidPtrToStringPtr(conversation.WorkspaceID),
		ConversationID: stringPtr(conversation.ID.String()),
		Message:        &apiMessage,
	}, true, nil
}

func (r *Runtime) hydrateConversationHit(ctx context.Context, auth domain.AuthContext, allowGlobal bool, item candidate) (api.SearchHit, bool, error) {
	conversationID, err := uuid.Parse(item.CanonicalID)
	if err != nil {
		return api.SearchHit{}, false, nil
	}
	conversation, err := r.loadConversation(ctx, conversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.SearchHit{}, false, nil
	}
	if err != nil {
		return api.SearchHit{}, false, err
	}
	visible, err := r.conversationVisible(ctx, auth.UserID, allowGlobal, conversation)
	if err != nil {
		return api.SearchHit{}, false, err
	}
	if !visible {
		return api.SearchHit{}, false, nil
	}
	apiConversation := conversationToAPI(conversation)
	return api.SearchHit{
		Kind:           documentKindConversation,
		ResourceID:     item.CanonicalID,
		Score:          item.Score,
		Title:          r.conversationTitle(ctx, conversation),
		Snippet:        trimOptionalStringValue(conversation.Description),
		WorkspaceID:    uuidPtrToStringPtr(conversation.WorkspaceID),
		ConversationID: stringPtr(conversation.ID.String()),
		Conversation:   &apiConversation,
	}, true, nil
}

func (r *Runtime) hydrateWorkspaceHit(ctx context.Context, auth domain.AuthContext, item candidate) (api.SearchHit, bool, error) {
	workspaceID, err := uuid.Parse(item.CanonicalID)
	if err != nil {
		return api.SearchHit{}, false, nil
	}
	visible, err := r.workspaceVisible(ctx, auth.UserID, workspaceID)
	if err != nil {
		return api.SearchHit{}, false, err
	}
	if !visible {
		return api.SearchHit{}, false, nil
	}
	workspace, err := r.loadWorkspace(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.SearchHit{}, false, nil
	}
	if err != nil {
		return api.SearchHit{}, false, err
	}
	apiWorkspace := workspaceToAPI(workspace)
	return api.SearchHit{
		Kind:        documentKindWorkspace,
		ResourceID:  item.CanonicalID,
		Score:       item.Score,
		Title:       stringPtr(workspace.Name),
		Snippet:     stringPtr(workspace.Slug),
		WorkspaceID: stringPtr(workspace.ID.String()),
		Workspace:   &apiWorkspace,
	}, true, nil
}

func (r *Runtime) hydrateUserHit(ctx context.Context, auth domain.AuthContext, item candidate) (api.SearchHit, bool, error) {
	userID, err := uuid.Parse(item.CanonicalID)
	if err != nil {
		return api.SearchHit{}, false, nil
	}
	var workspaceScope *uuid.UUID
	if item.WorkspaceID != nil {
		workspaceScope, _ = parseUUID(*item.WorkspaceID, "workspace_id")
	}
	var conversationScope *uuid.UUID
	if item.ConversationID != nil {
		conversationScope, _ = parseUUID(*item.ConversationID, "conversation_id")
	}
	visible, err := r.userVisible(ctx, auth.UserID, userID, workspaceScope, conversationScope)
	if err != nil {
		return api.SearchHit{}, false, err
	}
	if !visible {
		return api.SearchHit{}, false, nil
	}
	user, err := r.loadUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.SearchHit{}, false, nil
	}
	if err != nil {
		return api.SearchHit{}, false, err
	}
	apiUser := userToAPI(user)
	snippet := "@" + user.Handle
	if value := trimOptionalStringValue(user.Bio); value != nil {
		snippet = *value
	}
	return api.SearchHit{
		Kind:       documentKindUser,
		ResourceID: item.CanonicalID,
		Score:      item.Score,
		Title:      stringPtr(user.DisplayName),
		Snippet:    stringPtr(previewText(snippet, 220)),
		User:       &apiUser,
	}, true, nil
}

func (r *Runtime) hydrateEventHit(ctx context.Context, auth domain.AuthContext, item candidate) (api.SearchHit, bool, error) {
	eventID, err := parseEventID(item.CanonicalID)
	if err != nil {
		return api.SearchHit{}, false, nil
	}
	event, err := r.loadVisibleExternalEvent(ctx, auth.UserID, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.SearchHit{}, false, nil
	}
	if err != nil {
		return api.SearchHit{}, false, err
	}
	apiEvent := eventToAPI(event)
	return api.SearchHit{
		Kind:        documentKindEvent,
		ResourceID:  item.CanonicalID,
		Score:       item.Score,
		Title:       stringPtr(strings.ReplaceAll(event.Type, ".", " ")),
		Snippet:     stringPtr(previewText(flattenJSONText(event.Payload), 220)),
		WorkspaceID: uuidPtrToStringPtr(event.WorkspaceID),
		Event:       &apiEvent,
	}, true, nil
}

func (r *Runtime) embedDocuments(ctx context.Context, documents []searchDocument) []searchDocument {
	if r.embedder == nil || !r.embedder.Configured() || len(documents) == 0 {
		return documents
	}
	for start := 0; start < len(documents); start += embeddingBatchSize {
		end := start + embeddingBatchSize
		if end > len(documents) {
			end = len(documents)
		}
		texts := make([]string, 0, end-start)
		for _, document := range documents[start:end] {
			texts = append(texts, document.EmbeddingText)
		}
		vectors, err := r.embedder.EmbedDocuments(ctx, texts)
		if err != nil {
			r.logger.Warn("document embeddings failed; indexing BM25-only documents", "error", err, "batch_size", len(texts))
			continue
		}
		for i := range vectors {
			documents[start+i].Vector = vectors[i]
		}
	}
	return documents
}

func (r *Runtime) syncResource(ctx context.Context, resourceKind string, resourceID string) error {
	if !r.Configured() {
		return ErrNotConfigured
	}
	mutation, err := r.prepareSyncMutation(ctx, syncTarget{
		ResourceKind: resourceKind,
		ResourceID:   resourceID,
	})
	if err != nil {
		return err
	}
	mutation = r.embedPreparedMutations(ctx, []preparedMutation{mutation})[0]
	return r.applyNamespaceBatch(ctx, namespaceBatch{
		Namespace:  mutation.Namespace,
		ResultKeys: []string{mutation.ResultKey},
		Documents:  mutation.Documents,
	})
}

func (r *Runtime) syncEventFromSource(ctx context.Context, sourceInternalEventID uuid.UUID) error {
	if !r.Configured() {
		return ErrNotConfigured
	}
	mutation, err := r.prepareSyncMutation(ctx, syncTarget{SourceEventID: sourceInternalEventID.String()})
	if err != nil {
		return err
	}
	mutation = r.embedPreparedMutations(ctx, []preparedMutation{mutation})[0]
	return r.applyNamespaceBatch(ctx, namespaceBatch{
		Namespace:  mutation.Namespace,
		ResultKeys: []string{mutation.ResultKey},
		Documents:  mutation.Documents,
	})
}

func (r *Runtime) prepareSyncMutation(ctx context.Context, target syncTarget) (preparedMutation, error) {
	if strings.TrimSpace(target.SourceEventID) != "" {
		sourceInternalEventID, err := uuid.Parse(strings.TrimSpace(target.SourceEventID))
		if err != nil {
			return preparedMutation{}, err
		}
		documents, err := r.buildEventDocumentsFromSource(ctx, sourceInternalEventID)
		if err != nil {
			return preparedMutation{}, err
		}
		resourceID := ""
		if len(documents) > 0 {
			resourceID = documents[0].CanonicalID
		} else {
			event, err := r.loadExternalEventBySourceInternalEventID(ctx, sourceInternalEventID)
			if errors.Is(err, pgx.ErrNoRows) {
				return preparedMutation{}, fmt.Errorf("external event for source %s is not projected yet", sourceInternalEventID)
			}
			if err != nil {
				return preparedMutation{}, err
			}
			resourceID = event.ID.String()
		}
		target = syncTarget{
			ResourceKind:  documentKindEvent,
			ResourceID:    resourceID,
			SourceEventID: target.SourceEventID,
		}
		return preparedMutation{
			Target:    target,
			Namespace: r.namespaceName(documentCorpus(target.ResourceKind), target.ResourceID),
			ResultKey: resultKey(target.ResourceKind, target.ResourceID),
			Documents: documents,
		}, nil
	}

	documents, err := r.buildDocumentsForResource(ctx, target.ResourceKind, target.ResourceID)
	if err != nil {
		return preparedMutation{}, err
	}
	return preparedMutation{
		Target:    target,
		Namespace: r.namespaceName(documentCorpus(target.ResourceKind), target.ResourceID),
		ResultKey: resultKey(target.ResourceKind, target.ResourceID),
		Documents: documents,
	}, nil
}

func (r *Runtime) embedPreparedMutations(ctx context.Context, mutations []preparedMutation) []preparedMutation {
	if len(mutations) == 0 {
		return nil
	}
	offsets := make([]int, len(mutations)+1)
	documents := make([]searchDocument, 0)
	for i, mutation := range mutations {
		offsets[i] = len(documents)
		documents = append(documents, mutation.Documents...)
	}
	offsets[len(mutations)] = len(documents)
	documents = r.embedDocuments(ctx, documents)

	embedded := make([]preparedMutation, len(mutations))
	for i, mutation := range mutations {
		start := offsets[i]
		end := offsets[i+1]
		embedded[i] = mutation
		if end > start {
			embedded[i].Documents = append([]searchDocument(nil), documents[start:end]...)
		} else {
			embedded[i].Documents = nil
		}
	}
	return embedded
}

func (r *Runtime) applyNamespaceBatch(ctx context.Context, batch namespaceBatch) error {
	if err := r.deleteNamespaceResultKeys(ctx, batch.Namespace, batch.ResultKeys); err != nil {
		return err
	}
	if len(batch.Documents) == 0 {
		return nil
	}
	vectorDim := 0
	for _, document := range batch.Documents {
		if len(document.Vector) == 0 {
			continue
		}
		if vectorDim == 0 {
			vectorDim = len(document.Vector)
			continue
		}
		if len(document.Vector) != vectorDim {
			return fmt.Errorf("mixed vector dimensions for namespace %s", batch.Namespace)
		}
	}
	ns := r.tpuf.Namespace(batch.Namespace)
	schema := baseNamespaceSchema(vectorDim)
	for start := 0; start < len(batch.Documents); start += indexerUpsertBatch {
		end := start + indexerUpsertBatch
		if end > len(batch.Documents) {
			end = len(batch.Documents)
		}
		rows := make([]tpuf.RowParam, 0, end-start)
		for _, document := range batch.Documents[start:end] {
			rows = append(rows, rowFromDocument(document))
		}
		params := tpuf.NamespaceWriteParams{
			Namespace:  tpuf.String(batch.Namespace),
			Schema:     schema,
			UpsertRows: rows,
		}
		if vectorDim > 0 {
			params.DistanceMetric = tpuf.DistanceMetricCosineDistance
		}
		if _, err := ns.Write(ctx, params); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) deleteNamespaceResultKeys(ctx context.Context, namespace string, resultKeys []string) error {
	resultKeys = uniqueSortedStrings(resultKeys)
	if len(resultKeys) == 0 {
		return nil
	}
	ns := r.tpuf.Namespace(namespace)
	for start := 0; start < len(resultKeys); start += indexerDeleteBatch {
		end := start + indexerDeleteBatch
		if end > len(resultKeys) {
			end = len(resultKeys)
		}
		_, err := ns.Write(ctx, tpuf.NamespaceWriteParams{
			Namespace:      tpuf.String(namespace),
			DeleteByFilter: tpuf.NewFilterIn("result_key", resultKeys[start:end]),
		})
		if err != nil && !ignoreNamespaceSearchError(err) {
			return err
		}
	}
	return nil
}

func (r *Runtime) ensureNamespaceSchema(ctx context.Context, namespace string, vectorDim int) error {
	r.schemaMu.Lock()
	cachedDim, cached := r.namespaceVector[namespace]
	r.schemaMu.Unlock()

	if !cached {
		ns := r.tpuf.Namespace(namespace)
		schema, err := ns.Schema(ctx, tpuf.NamespaceSchemaParams{
			Namespace: tpuf.String(namespace),
		})
		if err != nil {
			if !ignoreNamespaceSearchError(err) {
				var apiErr *tpuf.Error
				if !(errors.As(err, &apiErr) && apiErr.StatusCode == 404) {
					return err
				}
			}
		} else {
			cachedDim = resolveVectorDim(*schema)
		}
		r.schemaMu.Lock()
		r.namespaceVector[namespace] = cachedDim
		r.schemaMu.Unlock()
	}

	desiredDim := vectorDim
	if desiredDim == 0 {
		desiredDim = cachedDim
	}
	if cached && cachedDim == desiredDim {
		return nil
	}

	ns := r.tpuf.Namespace(namespace)
	_, err := ns.UpdateSchema(ctx, tpuf.NamespaceUpdateSchemaParams{
		Namespace: tpuf.String(namespace),
		Schema:    baseNamespaceSchema(desiredDim),
	})
	if err != nil {
		return err
	}

	r.schemaMu.Lock()
	r.namespaceVector[namespace] = desiredDim
	r.schemaMu.Unlock()
	return nil
}
