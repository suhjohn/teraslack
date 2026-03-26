package e2e_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/suhjohn/teraslack/internal/domain"
)

func TestComposeE2E_ExternalEventsPaginationAndFiltering(t *testing.T) {
	ctx, pool, httpClient, baseURL, owner, _ := setupComposeE2EHTTP(t)
	ownerToken := createSessionToken(t, ctx, pool, owner.WorkspaceID, owner.ID)

	agentA := createUserViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateUserParams{
		Name:          uniqueName("events-a"),
		Email:         uniqueEmail("events-a"),
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       owner.ID,
		IsBot:         true,
	})
	agentB := createUserViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateUserParams{
		Name:          uniqueName("events-b"),
		Email:         uniqueEmail("events-b"),
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       owner.ID,
		IsBot:         true,
	})

	_, agentAKey := createAPIKeyViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateAPIKeyParams{
		Name:        "Events A Key",
		WorkspaceID: owner.WorkspaceID,
		UserID:      agentA.ID,
		CreatedBy:   owner.ID,
		Permissions: []string{
			domain.PermissionMessagesRead,
			domain.PermissionMessagesWrite,
			domain.PermissionConversationsCreate,
			domain.PermissionConversationsMembersWrite,
		},
	})

	channelA := createConversationViaHTTP(t, httpClient, baseURL, agentAKey, domain.CreateConversationParams{
		Name:      uniqueName("events-a"),
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: agentA.ID,
	})
	inviteUsersViaHTTP(t, httpClient, baseURL, agentAKey, channelA.ID, []string{agentB.ID})

	msgA1 := postMessageViaHTTP(t, httpClient, baseURL, agentAKey, domain.PostMessageParams{
		ChannelID: channelA.ID,
		UserID:    agentA.ID,
		Text:      "page one message",
	})
	msgA2 := postMessageViaHTTP(t, httpClient, baseURL, agentAKey, domain.PostMessageParams{
		ChannelID: channelA.ID,
		UserID:    agentA.ID,
		Text:      "page two message",
	})

	channelB := createConversationViaHTTP(t, httpClient, baseURL, agentAKey, domain.CreateConversationParams{
		Name:      uniqueName("events-b"),
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: agentA.ID,
	})
	msgB := postMessageViaHTTP(t, httpClient, baseURL, agentAKey, domain.PostMessageParams{
		ChannelID: channelB.ID,
		UserID:    agentA.ID,
		Text:      "other channel message",
	})

	filteredQuery := url.Values{
		"limit":         {"1"},
		"type":          {domain.EventTypeConversationMessageCreated},
		"resource_type": {domain.ResourceTypeConversation},
		"resource_id":   {channelA.ID},
	}
	page1 := waitForExternalEventPage(t, httpClient, baseURL, agentAKey, filteredQuery, 15*time.Second, func(page externalEventPage) bool {
		return len(page.Items) == 1 && page.HasMore && page.NextCursor != ""
	})

	page2Query := cloneValues(filteredQuery)
	page2Query.Set("after", page1.NextCursor)
	page2 := waitForExternalEventPage(t, httpClient, baseURL, agentAKey, page2Query, 15*time.Second, func(page externalEventPage) bool {
		return len(page.Items) == 1
	})

	if page2.Items[0].ID <= page1.Items[0].ID {
		t.Fatalf("second page event id = %d, want > %d", page2.Items[0].ID, page1.Items[0].ID)
	}

	allFilteredQuery := url.Values{
		"limit":         {"10"},
		"type":          {domain.EventTypeConversationMessageCreated},
		"resource_type": {domain.ResourceTypeConversation},
		"resource_id":   {channelA.ID},
	}
	allFiltered := waitForExternalEventPage(t, httpClient, baseURL, agentAKey, allFilteredQuery, 15*time.Second, func(page externalEventPage) bool {
		return len(page.Items) == 2 && !page.HasMore
	})

	wantTexts := map[string]struct{}{
		msgA1.Text: {},
		msgA2.Text: {},
	}
	for _, event := range allFiltered.Items {
		if event.Type != domain.EventTypeConversationMessageCreated {
			t.Fatalf("event type = %q, want %q", event.Type, domain.EventTypeConversationMessageCreated)
		}
		if event.ResourceType != domain.ResourceTypeConversation {
			t.Fatalf("event resource_type = %q, want %q", event.ResourceType, domain.ResourceTypeConversation)
		}
		if event.ResourceID != channelA.ID {
			t.Fatalf("event resource_id = %q, want %q", event.ResourceID, channelA.ID)
		}

		payload := decodeMessageEventPayload(t, event)
		if payload.ChannelID != channelA.ID {
			t.Fatalf("payload channel_id = %q, want %q", payload.ChannelID, channelA.ID)
		}
		if payload.UserID != agentA.ID {
			t.Fatalf("payload user_id = %q, want %q", payload.UserID, agentA.ID)
		}
		if _, ok := wantTexts[payload.Text]; !ok {
			t.Fatalf("unexpected payload text %q", payload.Text)
		}
		delete(wantTexts, payload.Text)
	}
	if len(wantTexts) != 0 {
		t.Fatalf("missing expected texts in filtered events: %v", wantTexts)
	}

	typeOnlyQuery := url.Values{
		"limit": {"10"},
		"type":  {domain.EventTypeConversationMessageCreated},
	}
	typeOnlyPage := waitForExternalEventPage(t, httpClient, baseURL, agentAKey, typeOnlyQuery, 15*time.Second, func(page externalEventPage) bool {
		return len(page.Items) == 3 && !page.HasMore
	})

	seenResourceIDs := map[string]int{}
	for _, event := range typeOnlyPage.Items {
		if event.Type != domain.EventTypeConversationMessageCreated {
			t.Fatalf("type-only event type = %q, want %q", event.Type, domain.EventTypeConversationMessageCreated)
		}
		seenResourceIDs[event.ResourceID]++
	}
	if seenResourceIDs[channelA.ID] != 2 || seenResourceIDs[channelB.ID] != 1 {
		t.Fatalf("type-only resource counts = %v, want %s=>2 and %s=>1", seenResourceIDs, channelA.ID, channelB.ID)
	}

	otherConversationQuery := url.Values{
		"limit":         {"10"},
		"type":          {domain.EventTypeConversationMessageCreated},
		"resource_type": {domain.ResourceTypeConversation},
		"resource_id":   {channelB.ID},
	}
	otherConversationPage := waitForExternalEventPage(t, httpClient, baseURL, agentAKey, otherConversationQuery, 15*time.Second, func(page externalEventPage) bool {
		return len(page.Items) == 1 && !page.HasMore
	})
	otherPayload := decodeMessageEventPayload(t, otherConversationPage.Items[0])
	if otherPayload.ChannelID != channelB.ID || otherPayload.UserID != agentA.ID || otherPayload.Text != msgB.Text {
		t.Fatalf("unexpected other conversation payload: %+v", otherPayload)
	}

	// Verify the endpoint still returns events after the projector has processed the workspace.
	rows, err := countExternalEventsViaHTTP(t, httpClient, baseURL, agentAKey, owner.WorkspaceID)
	if err != nil {
		t.Fatalf("count external events: %v", err)
	}
	if rows < 3 {
		t.Fatalf("external event count = %d, want at least 3", rows)
	}
}

func TestComposeE2E_WebhookExternalEventDelivery(t *testing.T) {
	ctx, pool, httpClient, baseURL, owner, _ := setupComposeE2EHTTP(t)
	ownerToken := createSessionToken(t, ctx, pool, owner.WorkspaceID, owner.ID)

	agentA := createUserViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateUserParams{
		Name:          uniqueName("webhook-a"),
		Email:         uniqueEmail("webhook-a"),
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       owner.ID,
		IsBot:         true,
	})

	_, agentAKey := createAPIKeyViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateAPIKeyParams{
		Name:        "Webhook A Key",
		WorkspaceID: owner.WorkspaceID,
		UserID:      agentA.ID,
		CreatedBy:   owner.ID,
		Permissions: []string{
			domain.PermissionMessagesRead,
			domain.PermissionMessagesWrite,
			domain.PermissionConversationsCreate,
			domain.PermissionConversationsMembersWrite,
		},
	})

	channelA := createConversationViaHTTP(t, httpClient, baseURL, agentAKey, domain.CreateConversationParams{
		Name:      uniqueName("webhook-a"),
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: agentA.ID,
	})
	channelB := createConversationViaHTTP(t, httpClient, baseURL, agentAKey, domain.CreateConversationParams{
		Name:      uniqueName("webhook-b"),
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: agentA.ID,
	})
	seed := postMessageViaHTTP(t, httpClient, baseURL, agentAKey, domain.PostMessageParams{
		ChannelID: channelA.ID,
		UserID:    agentA.ID,
		Text:      "seed before webhook",
	})

	receiverURL, collector := startComposeReachableWebhookServer(t)
	secret := uniqueName("webhook-secret")

	sub := createEventSubscriptionViaHTTP(t, httpClient, baseURL, agentAKey, map[string]any{
		"workspace_id":  owner.WorkspaceID,
		"url":           receiverURL,
		"type":          domain.EventTypeConversationMessageCreated,
		"resource_type": domain.ResourceTypeConversation,
		"resource_id":   channelA.ID,
		"secret":        secret,
	})
	if sub.ID == "" {
		t.Fatalf("create subscription returned empty id: %+v", sub)
	}

	addReactionViaHTTP(t, httpClient, baseURL, agentAKey, channelA.ID, seed.TS, "eyes", agentA.ID)
	postMessageViaHTTP(t, httpClient, baseURL, agentAKey, domain.PostMessageParams{
		ChannelID: channelB.ID,
		UserID:    agentA.ID,
		Text:      "wrong conversation",
	})
	collector.AssertCountStays(t, 0, 6*time.Second)

	wantMessage := postMessageViaHTTP(t, httpClient, baseURL, agentAKey, domain.PostMessageParams{
		ChannelID: channelA.ID,
		UserID:    agentA.ID,
		Text:      "deliver this message",
	})

	delivery := collector.WaitForCount(t, 1, 20*time.Second)[0]
	collector.AssertCountStays(t, 1, 4*time.Second)

	if got := delivery.Header.Get("X-Teraslack-Delivery-Id"); got == "" {
		t.Fatal("missing X-Teraslack-Delivery-Id")
	}
	if got := delivery.Header.Get("X-Teraslack-Request-Timestamp"); got == "" {
		t.Fatal("missing X-Teraslack-Request-Timestamp")
	}

	var event domain.ExternalEvent
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		t.Fatalf("decode webhook body: %v\nbody=%s", err, delivery.Body)
	}
	if event.WorkspaceID != owner.WorkspaceID {
		t.Fatalf("event workspace_id = %q, want %q", event.WorkspaceID, owner.WorkspaceID)
	}
	if event.Type != domain.EventTypeConversationMessageCreated {
		t.Fatalf("event type = %q, want %q", event.Type, domain.EventTypeConversationMessageCreated)
	}
	if event.ResourceType != domain.ResourceTypeConversation {
		t.Fatalf("event resource_type = %q, want %q", event.ResourceType, domain.ResourceTypeConversation)
	}
	if event.ResourceID != channelA.ID {
		t.Fatalf("event resource_id = %q, want %q", event.ResourceID, channelA.ID)
	}

	if got := delivery.Header.Get("X-Teraslack-Event-Id"); got != fmt.Sprintf("%d", event.ID) {
		t.Fatalf("X-Teraslack-Event-Id = %q, want %d", got, event.ID)
	}
	if got := delivery.Header.Get("X-Teraslack-Event-Type"); got != event.Type {
		t.Fatalf("X-Teraslack-Event-Type = %q, want %q", got, event.Type)
	}
	if got := delivery.Header.Get("X-Teraslack-Workspace-Id"); got != owner.WorkspaceID {
		t.Fatalf("X-Teraslack-Workspace-Id = %q, want %q", got, owner.WorkspaceID)
	}

	timestamp := delivery.Header.Get("X-Teraslack-Request-Timestamp")
	wantSig := computeWebhookSignature(secret, timestamp, delivery.Body)
	if got := delivery.Header.Get("X-Teraslack-Signature"); got != wantSig {
		t.Fatalf("X-Teraslack-Signature = %q, want %q", got, wantSig)
	}

	payload := decodeMessageEventPayload(t, event)
	if payload.ChannelID != channelA.ID || payload.UserID != agentA.ID || payload.Text != wantMessage.Text || payload.TS != wantMessage.TS {
		t.Fatalf("unexpected webhook payload: %+v", payload)
	}
}

func TestComposeE2E_ExternalEventsHonorAPIKeyReadPermissions(t *testing.T) {
	_, _, httpClient, baseURL, owner, ownerToken := setupComposeE2EHTTP(t)

	agent := createUserViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateUserParams{
		Name:          uniqueName("events-authz"),
		Email:         uniqueEmail("events-authz"),
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       owner.ID,
		IsBot:         true,
	})

	_, writerKey := createAPIKeyViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateAPIKeyParams{
		Name:        "Events Writer Key",
		WorkspaceID: owner.WorkspaceID,
		UserID:      agent.ID,
		CreatedBy:   owner.ID,
		Permissions: []string{
			domain.PermissionMessagesRead,
			domain.PermissionMessagesWrite,
			domain.PermissionConversationsCreate,
		},
	})
	_, limitedKey := createAPIKeyViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateAPIKeyParams{
		Name:        "Events Limited Key",
		WorkspaceID: owner.WorkspaceID,
		UserID:      agent.ID,
		CreatedBy:   owner.ID,
		Permissions: []string{
			domain.PermissionConversationsCreate,
		},
	})

	channel := createConversationViaHTTP(t, httpClient, baseURL, writerKey, domain.CreateConversationParams{
		Name:      uniqueName("events-authz"),
		Type:      domain.ConversationTypePublicChannel,
		CreatorID: agent.ID,
	})
	postMessageViaHTTP(t, httpClient, baseURL, writerKey, domain.PostMessageParams{
		ChannelID: channel.ID,
		UserID:    agent.ID,
		Text:      "visible only to readers",
	})

	query := url.Values{
		"type":          {domain.EventTypeConversationMessageCreated},
		"resource_type": {domain.ResourceTypeConversation},
		"resource_id":   {channel.ID},
	}
	page := waitForExternalEventPage(t, httpClient, baseURL, limitedKey, query, 15*time.Second, func(page externalEventPage) bool {
		return len(page.Items) == 0 && !page.HasMore
	})
	if len(page.Items) != 0 {
		t.Fatalf("limited key saw %d conversation events, want none", len(page.Items))
	}
}

type externalEventPage struct {
	Items      []domain.ExternalEvent `json:"items"`
	NextCursor string                 `json:"next_cursor"`
	HasMore    bool                   `json:"has_more"`
}

type messageEventPayload struct {
	TS        string `json:"ts"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	Text      string `json:"text"`
}

type deliveredWebhook struct {
	Header http.Header
	Body   []byte
}

type webhookCollector struct {
	mu         sync.Mutex
	deliveries []deliveredWebhook
	notify     chan struct{}
}

func setupComposeE2EHTTP(t *testing.T) (context.Context, *pgxpool.Pool, *http.Client, string, *domain.User, string) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping compose e2e test in short mode")
	}
	if os.Getenv("TERASLACK_E2E") != "1" {
		t.Skip("set TERASLACK_E2E=1 to run compose-backed e2e tests")
	}

	ctx := context.Background()
	baseURL := e2eBaseURL()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Fatal("DATABASE_URL is required for compose-backed e2e tests")
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	owner := bootstrapOwnerUser(t, ctx, pool)
	httpClient := &http.Client{Timeout: 10 * time.Second}
	ownerToken := createSessionToken(t, ctx, pool, owner.WorkspaceID, owner.ID)
	return ctx, pool, httpClient, baseURL, owner, ownerToken
}

func listExternalEventsPageViaHTTP(t *testing.T, httpClient *http.Client, baseURL, auth string, query url.Values) externalEventPage {
	t.Helper()
	rawQuery := query.Encode()
	endpoint := baseURL + "/events"
	if rawQuery != "" {
		endpoint += "?" + rawQuery
	}
	var resp externalEventPage
	doJSON(t, httpClient, http.MethodGet, endpoint, auth, nil, &resp)
	if resp.Items == nil {
		resp.Items = []domain.ExternalEvent{}
	}
	return resp
}

func waitForExternalEventPage(t *testing.T, httpClient *http.Client, baseURL, auth string, query url.Values, timeout time.Duration, predicate func(externalEventPage) bool) externalEventPage {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var last externalEventPage
	for time.Now().Before(deadline) {
		last = listExternalEventsPageViaHTTP(t, httpClient, baseURL, auth, query)
		if predicate(last) {
			return last
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for /events query %q; last response: has_more=%v next_cursor=%q items=%d", query.Encode(), last.HasMore, last.NextCursor, len(last.Items))
	return externalEventPage{}
}

func decodeMessageEventPayload(t *testing.T, event domain.ExternalEvent) messageEventPayload {
	t.Helper()
	var payload messageEventPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode event payload for event %d: %v\npayload=%s", event.ID, err, event.Payload)
	}
	return payload
}

func cloneValues(in url.Values) url.Values {
	out := make(url.Values, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func createEventSubscriptionViaHTTP(t *testing.T, httpClient *http.Client, baseURL, auth string, body map[string]any) domain.EventSubscription {
	t.Helper()
	var resp domain.EventSubscription
	doJSON(t, httpClient, http.MethodPost, baseURL+"/event-subscriptions", auth, body, &resp)
	return resp
}

func startComposeReachableWebhookServer(t *testing.T) (string, *webhookCollector) {
	t.Helper()

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen for webhook receiver: %v", err)
	}

	collector := &webhookCollector{notify: make(chan struct{}, 1)}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer r.Body.Close()
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			collector.mu.Lock()
			collector.deliveries = append(collector.deliveries, deliveredWebhook{
				Header: r.Header.Clone(),
				Body:   append([]byte(nil), body...),
			})
			collector.mu.Unlock()

			select {
			case collector.notify <- struct{}{}:
			default:
			}

			w.WriteHeader(http.StatusNoContent)
		}),
	}

	go func() {
		_ = server.Serve(listener)
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})

	host := os.Getenv("TERASLACK_E2E_WEBHOOK_HOST")
	if host == "" {
		host = "host.docker.internal"
	}
	return fmt.Sprintf("http://%s:%d", host, listener.Addr().(*net.TCPAddr).Port), collector
}

func (c *webhookCollector) WaitForCount(t *testing.T, want int, timeout time.Duration) []deliveredWebhook {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		got := c.snapshot()
		if len(got) >= want {
			return got
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("timed out waiting for %d webhook deliveries; got %d", want, len(got))
		}
		select {
		case <-c.notify:
		case <-time.After(minDuration(remaining, 250*time.Millisecond)):
		}
	}
}

func (c *webhookCollector) AssertCountStays(t *testing.T, want int, duration time.Duration) {
	t.Helper()

	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		got := c.snapshot()
		if len(got) != want {
			t.Fatalf("webhook delivery count = %d, want %d", len(got), want)
		}
		select {
		case <-c.notify:
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (c *webhookCollector) snapshot() []deliveredWebhook {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]deliveredWebhook, len(c.deliveries))
	copy(out, c.deliveries)
	return out
}

func computeWebhookSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("v0:%s:%s", timestamp, body)))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func countExternalEventsViaHTTP(t *testing.T, httpClient *http.Client, baseURL, auth, workspaceID string) (int, error) {
	t.Helper()

	page := listExternalEventsPageViaHTTP(t, httpClient, baseURL, auth, url.Values{"limit": {"100"}})
	count := 0
	for {
		count += len(page.Items)
		if !page.HasMore || page.NextCursor == "" {
			return count, nil
		}
		page = listExternalEventsPageViaHTTP(t, httpClient, baseURL, auth, url.Values{
			"limit": {"100"},
			"after": {page.NextCursor},
		})
		if count > 10000 {
			return 0, fmt.Errorf("unexpectedly large event count for workspace %s", workspaceID)
		}
	}
}
