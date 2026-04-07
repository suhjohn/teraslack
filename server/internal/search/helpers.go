package search

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	tpuf "github.com/turbopuffer/turbopuffer-go"
	"github.com/turbopuffer/turbopuffer-go/packages/param"

	"github.com/johnsuh/teraslack/server/internal/api"
)

func parseCursor(raw string) (*searchCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, malformed("Malformed cursor.")
	}
	var cursor searchCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return nil, malformed("Malformed cursor.")
	}
	return &cursor, nil
}

func encodeCursor(cursor searchCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func normalizeKinds(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	items := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(strings.ToLower(item))
		switch item {
		case documentKindMessage, documentKindConversation, documentKindWorkspace, documentKindUser, documentKindEvent:
		default:
			return nil, invalid("kinds", "invalid_value", fmt.Sprintf("Unsupported kind %q.", item))
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		items = append(items, item)
	}
	sort.Strings(items)
	return items, nil
}

func corporaForKinds(kinds []string) []string {
	if len(kinds) == 0 {
		return []string{contentCorpus, entityCorpus}
	}
	needsContent := false
	needsEntity := false
	for _, kind := range kinds {
		switch kind {
		case documentKindMessage, documentKindEvent:
			needsContent = true
		default:
			needsEntity = true
		}
	}
	items := make([]string, 0, 2)
	if needsContent {
		items = append(items, contentCorpus)
	}
	if needsEntity {
		items = append(items, entityCorpus)
	}
	return items
}

func (r *Runtime) namespaceName(corpus string, canonicalID string) string {
	shardCount := entityShardCount
	if corpus == contentCorpus {
		shardCount = contentShardCount
	}
	shard := shardForKey(canonicalID, shardCount)
	return fmt.Sprintf("%s-search-%s-v1-%02d", r.cfg.TurbopufferNSPrefix, corpus, shard)
}

func shardForKey(key string, shardCount int) int {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(key))
	return int(hasher.Sum32() % uint32(shardCount))
}

func documentCorpus(kind string) string {
	switch kind {
	case documentKindMessage, documentKindEvent:
		return contentCorpus
	default:
		return entityCorpus
	}
}

func resultKey(kind string, canonicalID string) string {
	return kind + ":" + canonicalID
}

func documentID(kind string, canonicalID string, anchor documentAnchor) string {
	return uuid.NewSHA1(searchDocumentNamespace, []byte(resultKey(kind, canonicalID)+"|"+anchor.AnchorKey)).String()
}

func (t syncTarget) key() string {
	if t.SourceEventID > 0 {
		return "source:" + int64String(t.SourceEventID)
	}
	return resultKey(t.ResourceKind, t.ResourceID)
}

func readJSONMap(raw []byte) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func flattenJSONText(value any) string {
	parts := make([]string, 0, 8)
	appendFlattenedText(&parts, value)
	return strings.Join(parts, " ")
}

func appendFlattenedText(parts *[]string, value any) {
	switch typed := value.(type) {
	case string:
		typed = strings.TrimSpace(typed)
		if typed != "" {
			*parts = append(*parts, typed)
		}
	case []any:
		for _, item := range typed {
			appendFlattenedText(parts, item)
		}
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			appendFlattenedText(parts, typed[key])
		}
	}
}

func messageText(message messageRow) string {
	text := strings.TrimSpace(message.BodyText)
	if text != "" {
		return text
	}
	return strings.TrimSpace(flattenJSONText(message.BodyRich))
}

func previewText(text string, limit int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}

func trimOptionalStringValue(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func nonEmpty(parts ...string) []string {
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	return items
}

func stringPtr(value string) *string {
	return &value
}

func uuidPtrToStringPtr(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	out := value.String()
	return &out
}

func timePtrToStringPtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	out := value.UTC().Format(time.RFC3339)
	return &out
}

func messageToAPI(row messageRow) api.Message {
	return api.Message{
		ID:             row.ID.String(),
		ConversationID: row.ConversationID.String(),
		AuthorUserID:   row.AuthorUserID.String(),
		BodyText:       row.BodyText,
		BodyRich:       row.BodyRich,
		Metadata:       row.Metadata,
		EditedAt:       timePtrToStringPtr(row.EditedAt),
		DeletedAt:      timePtrToStringPtr(row.DeletedAt),
		CreatedAt:      row.CreatedAt.Format(time.RFC3339),
	}
}

func conversationToAPI(row conversationRow) api.Conversation {
	return api.Conversation{
		ID:               row.ID.String(),
		WorkspaceID:      uuidPtrToStringPtr(row.WorkspaceID),
		AccessPolicy:     row.AccessPolicy,
		ParticipantCount: row.ParticipantCount,
		Title:            row.Title,
		Description:      row.Description,
		CreatedByUserID:  row.CreatedByUserID.String(),
		Archived:         row.ArchivedAt != nil,
		LastMessageAt:    timePtrToStringPtr(row.LastMessageAt),
		CreatedAt:        row.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        row.UpdatedAt.Format(time.RFC3339),
	}
}

func workspaceToAPI(row workspaceRow) api.Workspace {
	return api.Workspace{
		ID:              row.ID.String(),
		Slug:            row.Slug,
		Name:            row.Name,
		CreatedByUserID: row.CreatedByUserID.String(),
		CreatedAt:       row.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Format(time.RFC3339),
	}
}

func userToAPI(row userRow) api.User {
	return api.User{
		ID:            row.ID.String(),
		PrincipalType: row.PrincipalType,
		Status:        row.Status,
		Email:         row.Email,
		Profile: api.UserProfile{
			Handle:      row.Handle,
			DisplayName: row.DisplayName,
			AvatarURL:   row.AvatarURL,
			Bio:         row.Bio,
		},
	}
}

func eventToAPI(row externalEventRow) api.ExternalEvent {
	return api.ExternalEvent{
		ID:           row.ID,
		WorkspaceID:  uuidPtrToStringPtr(row.WorkspaceID),
		Type:         row.Type,
		ResourceType: row.ResourceType,
		ResourceID:   row.ResourceID.String(),
		OccurredAt:   row.OccurredAt.Format(time.RFC3339),
		Payload:      row.Payload,
	}
}

func optionalString(value any) *string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return &typed
	default:
		return nil
	}
}

func tpufIncludeAttributes(fields ...string) tpuf.IncludeAttributesParam {
	return tpuf.IncludeAttributesParam{StringArray: fields}
}

func tpufStrongQueryConsistency() tpuf.NamespaceQueryParamsConsistency {
	return tpuf.NamespaceQueryParamsConsistency{
		Level: tpuf.NamespaceQueryParamsConsistencyLevelStrong,
	}
}

func tpufStrongMultiQueryConsistency() tpuf.NamespaceMultiQueryParamsConsistency {
	return tpuf.NamespaceMultiQueryParamsConsistency{
		Level: tpuf.NamespaceMultiQueryParamsConsistencyLevelStrong,
	}
}

func tpufBool(value bool) param.Opt[bool] {
	return tpuf.Bool(value)
}

func tpufInt(value int64) param.Opt[int64] {
	return tpuf.Int(value)
}

func loadTime(value any) time.Time {
	switch typed := value.(type) {
	case string:
		if parsed, err := time.Parse(time.RFC3339, typed); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func currentUpdatedAt(createdAt time.Time, updatedAt *time.Time) time.Time {
	if updatedAt != nil && !updatedAt.IsZero() {
		return updatedAt.UTC()
	}
	return createdAt.UTC()
}

func conversationAccessAnchors(conversation conversationRow) []documentAnchor {
	switch {
	case conversation.WorkspaceID == nil && conversation.AccessPolicy == "authenticated":
		return []documentAnchor{{
			PrincipalID: authenticatedPrincipalID(),
			AnchorKey:   "authenticated",
		}}
	case conversation.AccessPolicy == "workspace" && conversation.WorkspaceID != nil:
		return []documentAnchor{{
			PrincipalID: workspacePrincipalID(*conversation.WorkspaceID),
			WorkspaceID: conversation.WorkspaceID,
			AnchorKey:   "workspace:" + conversation.WorkspaceID.String(),
		}}
	default:
		return []documentAnchor{{
			PrincipalID:    conversationPrincipalID(conversation.ID),
			WorkspaceID:    conversation.WorkspaceID,
			ConversationID: &conversation.ID,
			AnchorKey:      "conversation:" + conversation.ID.String(),
		}}
	}
}

func baseNamespaceSchema(vectorDim int) map[string]tpuf.AttributeSchemaConfigParam {
	schema := map[string]tpuf.AttributeSchemaConfigParam{
		"kind": {
			Type:       "string",
			Filterable: tpufBool(true),
		},
		"canonical_id": {
			Type:       "string",
			Filterable: tpufBool(true),
		},
		"result_key": {
			Type:       "string",
			Filterable: tpufBool(true),
		},
		"workspace_id": {
			Type:       "uuid",
			Filterable: tpufBool(true),
		},
		"conversation_id": {
			Type:       "uuid",
			Filterable: tpufBool(true),
		},
		"read_principal_ids": {
			Type:       "[]uuid",
			Filterable: tpufBool(true),
		},
		"title": {
			Type:           "string",
			FullTextSearch: &tpuf.FullTextSearchConfigParam{Language: tpuf.LanguageEnglish, AsciiFolding: tpufBool(true)},
			Filterable:     tpufBool(false),
		},
		"body": {
			Type:           "string",
			FullTextSearch: &tpuf.FullTextSearchConfigParam{Language: tpuf.LanguageEnglish, AsciiFolding: tpufBool(true)},
			Filterable:     tpufBool(false),
		},
		"exact_terms": {
			Type:           "[]string",
			FullTextSearch: &tpuf.FullTextSearchConfigParam{Language: tpuf.LanguageEnglish, AsciiFolding: tpufBool(true)},
			Filterable:     tpufBool(false),
		},
		"created_at": {
			Type:       "datetime",
			Filterable: tpufBool(true),
		},
		"updated_at": {
			Type:       "datetime",
			Filterable: tpufBool(true),
		},
		"archived": {
			Type:       "bool",
			Filterable: tpufBool(true),
		},
	}
	if vectorDim > 0 {
		schema["vector"] = tpuf.AttributeSchemaConfigParam{
			Type: fmt.Sprintf("[%d]f32", vectorDim),
			Ann: tpuf.AttributeSchemaConfigAnnParam{
				DistanceMetric: tpuf.DistanceMetricCosineDistance,
			},
		}
	}
	return schema
}

func rowFromDocument(document searchDocument) tpuf.RowParam {
	row := tpuf.RowParam{
		"id":                 document.DocID,
		"kind":               document.Kind,
		"canonical_id":       document.CanonicalID,
		"result_key":         document.ResultKey,
		"read_principal_ids": uuidStrings(document.ReadPrincipalIDs),
		"title":              document.Title,
		"body":               document.Body,
		"exact_terms":        document.ExactTerms,
		"created_at":         document.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":         document.UpdatedAt.UTC().Format(time.RFC3339),
		"archived":           document.Archived,
	}
	if document.WorkspaceID != nil {
		row["workspace_id"] = document.WorkspaceID.String()
	}
	if document.ConversationID != nil {
		row["conversation_id"] = document.ConversationID.String()
	}
	if len(document.Vector) > 0 {
		row["vector"] = document.Vector
	}
	return row
}

func uuidStrings(values []uuid.UUID) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		items = append(items, value.String())
	}
	return items
}

func appendQueryFilter(filters []tpuf.Filter, filter tpuf.Filter) []tpuf.Filter {
	if filter == nil {
		return filters
	}
	return append(filters, filter)
}

func mergeDocumentsByID(documents []searchDocument) []searchDocument {
	seen := map[string]searchDocument{}
	for _, document := range documents {
		seen[document.DocID] = document
	}
	items := make([]searchDocument, 0, len(seen))
	for _, document := range seen {
		items = append(items, document)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].DocID < items[j].DocID
	})
	return items
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]struct{}{}
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}

func parseUUID(value string, field string) (*uuid.UUID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return nil, invalid(field, "invalid_uuid", "Must be a valid UUID.")
	}
	return &parsed, nil
}

func int64String(value int64) string {
	return fmt.Sprintf("%d", value)
}

func sameStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func resolveVectorDim(schema tpuf.NamespaceSchemaResponse) int {
	vectorAttr, ok := schema["vector"]
	if !ok {
		return 0
	}
	attrType := strings.TrimSpace(vectorAttr.Type)
	if !strings.HasPrefix(attrType, "[") || !strings.Contains(attrType, "]f32") {
		return 0
	}
	end := strings.Index(attrType, "]")
	if end <= 1 {
		return 0
	}
	var dim int
	_, _ = fmt.Sscanf(attrType[1:end], "%d", &dim)
	return dim
}

func sortCandidates(items []candidate) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].CanonicalID < items[j].CanonicalID
	})
}

func namedDocumentTerms(parts ...string) []string {
	items := make([]string, 0, len(parts)*2)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		items = append(items, part)
		if strings.HasPrefix(part, "@") {
			items = append(items, strings.TrimPrefix(part, "@"))
		}
	}
	return dedupeStrings(items)
}

func rrfScore(rank int, weight float64) float64 {
	return weight / float64(fusionK+rank+1)
}
