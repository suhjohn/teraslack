package search

import (
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	tpuf "github.com/turbopuffer/turbopuffer-go"

	"github.com/johnsuh/teraslack/server/internal/config"
	"github.com/johnsuh/teraslack/server/internal/dbsqlc"
	"github.com/johnsuh/teraslack/server/internal/embedding"
)

const (
	contentShardCount  = 8
	entityShardCount   = 4
	defaultLimit       = 20
	maxLimit           = 50
	fusionK            = 60
	embeddingBatchSize = 128

	contentCorpus = "content"
	entityCorpus  = "entity"

	documentKindMessage      = "message"
	documentKindConversation = "conversation"
	documentKindWorkspace    = "workspace"
	documentKindUser         = "user"
	documentKindEvent        = "event"

	indexerLeaseName     = "search-indexer"
	indexerJobKind       = "search_sync"
	indexerBatchSize     = 200
	indexerWorkerBatch   = 100
	indexerDeleteBatch   = 100
	indexerUpsertBatch   = 250
	indexerRetryDelay    = 15 * time.Second
	indexerHeartbeatFreq = 30 * time.Second
)

type Runtime struct {
	cfg      config.Config
	db       *pgxpool.Pool
	queries  *dbsqlc.Queries
	logger   *slog.Logger
	tpuf     tpuf.Client
	embedder *embedding.Client

	schemaMu        sync.Mutex
	namespaceVector map[string]int
}

type searchCursor struct {
	Query          string   `json:"query"`
	Kinds          []string `json:"kinds,omitempty"`
	WorkspaceID    string   `json:"workspace_id,omitempty"`
	ConversationID string   `json:"conversation_id,omitempty"`
	Seen           []string `json:"seen,omitempty"`
}

type candidate struct {
	Kind           string
	CanonicalID    string
	ResultKey      string
	WorkspaceID    *string
	ConversationID *string
	Score          float64
}

type userRow struct {
	ID            uuid.UUID
	PrincipalType string
	Status        string
	Email         *string
	Handle        string
	DisplayName   string
	AvatarURL     *string
	Bio           *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type workspaceRow struct {
	ID              uuid.UUID
	Slug            string
	Name            string
	CreatedByUserID uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type conversationRow struct {
	ID               uuid.UUID
	WorkspaceID      *uuid.UUID
	AccessPolicy     string
	Title            *string
	Description      *string
	CreatedByUserID  uuid.UUID
	ArchivedAt       *time.Time
	LastMessageAt    *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ParticipantCount int
}

type messageRow struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	AuthorUserID   uuid.UUID
	BodyText       string
	BodyRich       map[string]any
	Metadata       map[string]any
	EditedAt       *time.Time
	DeletedAt      *time.Time
	CreatedAt      time.Time
}

type externalEventRow struct {
	ID                    uuid.UUID
	WorkspaceID           *uuid.UUID
	Type                  string
	ResourceType          string
	ResourceID            uuid.UUID
	OccurredAt            time.Time
	Payload               map[string]any
	SourceInternalEventID *uuid.UUID
}

type documentAnchor struct {
	PrincipalID    uuid.UUID
	WorkspaceID    *uuid.UUID
	ConversationID *uuid.UUID
	AnchorKey      string
}

type searchDocument struct {
	Kind             string
	CanonicalID      string
	ResultKey        string
	DocID            string
	WorkspaceID      *uuid.UUID
	ConversationID   *uuid.UUID
	ReadPrincipalIDs []uuid.UUID
	Title            string
	Body             string
	ExactTerms       []string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Archived         bool
	EmbeddingText    string
	Vector           []float32
}

type syncJobPayload struct {
	ResourceKind  string `json:"resource_kind,omitempty"`
	ResourceID    string `json:"resource_id,omitempty"`
	SourceEventID string `json:"source_event_id,omitempty"`
}

type syncTarget struct {
	ResourceKind  string
	ResourceID    string
	SourceEventID string
}

type preparedMutation struct {
	Target    syncTarget
	Namespace string
	ResultKey string
	Documents []searchDocument
}

type namespaceBatch struct {
	Namespace  string
	ResultKeys []string
	Documents  []searchDocument
}
