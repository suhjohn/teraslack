//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	tpuf "github.com/turbopuffer/turbopuffer-go"
	"github.com/turbopuffer/turbopuffer-go/option"

	"github.com/johnsuh/teraslack/server/internal/api"
	teracrypto "github.com/johnsuh/teraslack/server/internal/crypto"
)

const (
	testEncryptionKey               = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	eventualTimeout                 = 30 * time.Second
	searchWaitTimeout               = 60 * time.Second
	pollInterval                    = 500 * time.Millisecond
	liveSearchEnvFlag               = "INTEGRATION_LIVE_SEARCH"
	integrationTurbopufferAPIKeyEnv = "INTEGRATION_TURBOPUFFER_API_KEY"
	integrationTurbopufferRegionEnv = "INTEGRATION_TURBOPUFFER_REGION"
	integrationTurbopufferPrefixEnv = "INTEGRATION_TURBOPUFFER_NS_PREFIX"
	integrationModalAPIKeyEnv       = "INTEGRATION_MODAL_SERVER_API_KEY"
	integrationModalServerURLEnv    = "INTEGRATION_MODAL_EMBEDDING_SERVER_URL"
)

var testStack *composeStack

type composeStack struct {
	rootDir            string
	stateEnvPath       string
	projectName        string
	baseURL            string
	databaseURL        string
	webhookRecorderURL string
	pool               *pgxpool.Pool
	liveSearch         *liveSearchConfig
}

type liveSearchConfig struct {
	TurbopufferAPIKey string
	TurbopufferRegion string
	NamespacePrefix   string
	ModalServerAPIKey string
	ModalEmbeddingURL string
}

type workflowHarness struct {
	t         *testing.T
	pool      *pgxpool.Pool
	client    *http.Client
	baseURL   string
	namespace string
	protector *teracrypto.StringProtector
	stack     *composeStack
}

type actor struct {
	User  api.User
	Token string
	Email string
}

type webhookRecorder struct {
	adminURL     string
	containerURL string
	client       *http.Client
}

type webhookRecord struct {
	Body      []byte
	Headers   http.Header
	Signature string
}

type webhookRecorderResponse struct {
	Records []webhookRecordPayload `json:"records"`
	DelayMS int                    `json:"delay_ms"`
}

type webhookRecordPayload struct {
	BodyBase64 string              `json:"body_base64"`
	Headers    map[string][]string `json:"headers"`
	Signature  string              `json:"signature"`
}

func TestMain(m *testing.M) {
	stack, err := startComposeStack()
	if err != nil {
		fmt.Fprintf(os.Stderr, "start compose stack: %v\n", err)
		os.Exit(1)
	}
	testStack = stack

	code := m.Run()

	if err := stack.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "stop compose stack: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func startComposeStack() (*composeStack, error) {
	rootDir, err := repoRoot()
	if err != nil {
		return nil, err
	}

	stateDir := filepath.Join(rootDir, "tmp", "integration_test")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("create integration test state dir: %w", err)
	}

	usedPorts := map[string]bool{}
	dbPort, err := pickUnusedPort(usedPorts)
	if err != nil {
		return nil, err
	}
	apiPort, err := pickUnusedPort(usedPorts)
	if err != nil {
		return nil, err
	}
	minioAPIPort, err := pickUnusedPort(usedPorts)
	if err != nil {
		return nil, err
	}
	minioConsolePort, err := pickUnusedPort(usedPorts)
	if err != nil {
		return nil, err
	}
	webhookRecorderPort, err := pickUnusedPort(usedPorts)
	if err != nil {
		return nil, err
	}

	runID := "it" + time.Now().UTC().Format("20060102150405") + shortID()
	projectName := "teraslack-" + runID
	stateEnvPath := filepath.Join(stateDir, runID+".env")
	baseURL := fmt.Sprintf("http://127.0.0.1:%s", apiPort)
	databaseURL := fmt.Sprintf("postgres://slackbackend:slackbackend@127.0.0.1:%s/slackbackend?sslmode=disable", dbPort)
	webhookRecorderURL := fmt.Sprintf("http://127.0.0.1:%s", webhookRecorderPort)
	liveSearch, err := loadLiveSearchConfig(runID)
	if err != nil {
		return nil, err
	}

	stateEnvLines := []string{
		"DB_PORT=" + dbPort,
		"API_PORT=" + apiPort,
		"COMPOSE_PROJECT_NAME=" + projectName,
		"BASE_URL=" + baseURL,
		"ENCRYPTION_KEY=" + testEncryptionKey,
		"MINIO_API_PORT=" + minioAPIPort,
		"MINIO_CONSOLE_PORT=" + minioConsolePort,
		"WEBHOOK_RECORDER_PORT=" + webhookRecorderPort,
		"S3_REGION=us-east-1",
		"S3_ENDPOINT=http://minio:9000",
		"S3_BUCKET=teraslack-integration",
		"S3_ACCESS_KEY=teraslack",
		"S3_SECRET_KEY=teraslack-secret",
	}
	if liveSearch != nil {
		stateEnvLines = append(stateEnvLines,
			"MODAL_SERVER_API_KEY="+liveSearch.ModalServerAPIKey,
			"MODAL_EMBEDDING_SERVER_URL="+liveSearch.ModalEmbeddingURL,
			"TURBOPUFFER_API_KEY="+liveSearch.TurbopufferAPIKey,
			"TURBOPUFFER_REGION="+liveSearch.TurbopufferRegion,
			"TURBOPUFFER_NS_PREFIX="+liveSearch.NamespacePrefix,
		)
	}
	stateEnvLines = append(stateEnvLines, "")
	stateEnv := strings.Join(stateEnvLines, "\n")
	if err := os.WriteFile(stateEnvPath, []byte(stateEnv), 0o644); err != nil {
		return nil, fmt.Errorf("write integration env file: %w", err)
	}

	stack := &composeStack{
		rootDir:            rootDir,
		stateEnvPath:       stateEnvPath,
		projectName:        projectName,
		baseURL:            baseURL,
		databaseURL:        databaseURL,
		webhookRecorderURL: webhookRecorderURL,
		liveSearch:         liveSearch,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if _, err := stack.runCompose(ctx, "up", "--build", "-d"); err != nil {
		_ = stack.Close()
		return nil, err
	}

	if err := waitForHTTP(stack.baseURL+"/healthz", 2*time.Minute); err != nil {
		_ = stack.dumpComposeState()
		_ = stack.Close()
		return nil, err
	}
	if err := waitForHTTP(stack.webhookRecorderURL+"/_admin/records", 1*time.Minute); err != nil {
		_ = stack.dumpComposeState()
		_ = stack.Close()
		return nil, err
	}

	pool, err := waitForPostgres(stack.databaseURL, 90*time.Second)
	if err != nil {
		_ = stack.dumpComposeState()
		_ = stack.Close()
		return nil, err
	}
	stack.pool = pool

	return stack, nil
}

func (s *composeStack) Close() error {
	if s == nil {
		return nil
	}
	errs := make([]error, 0, 2)
	if s.pool != nil {
		s.pool.Close()
	}
	if s.rootDir != "" && s.stateEnvPath != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := s.runCompose(ctx, "down", "-v", "--remove-orphans"); err != nil {
			errs = append(errs, err)
		}
	}
	if s.liveSearch != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := s.cleanupLiveSearchNamespaces(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if s.stateEnvPath != "" {
		_ = os.Remove(s.stateEnvPath)
	}
	return errors.Join(errs...)
}

func (s *composeStack) runCompose(ctx context.Context, args ...string) (string, error) {
	cmdArgs := append([]string{"compose", "--env-file", s.stateEnvPath, "-p", s.projectName}, args...)
	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	cmd.Dir = s.rootDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(cmdArgs, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func (s *composeStack) dumpComposeState() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if output, err := s.runCompose(ctx, "ps"); err == nil {
		fmt.Fprintf(os.Stderr, "compose ps:\n%s\n", output)
	}
	if output, err := s.runCompose(ctx, "logs", "--tail", "100"); err == nil {
		fmt.Fprintf(os.Stderr, "compose logs:\n%s\n", output)
	}
	return nil
}

func (s *composeStack) ScaleService(ctx context.Context, service string, count int) error {
	if _, err := s.runCompose(ctx, "up", "-d", "--scale", fmt.Sprintf("%s=%d", service, count), service); err != nil {
		return err
	}
	return s.waitForServiceCount(ctx, service, count)
}

func (s *composeStack) waitForServiceCount(ctx context.Context, service string, count int) error {
	deadline := time.Now().Add(1 * time.Minute)
	var lastCount int
	for time.Now().Before(deadline) {
		output, err := s.runCompose(ctx, "ps", "-q", service)
		if err == nil {
			lastCount = countNonEmptyLines(output)
			if lastCount >= count {
				return nil
			}
		}
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("service %s reached %d instances, want at least %d", service, lastCount, count)
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve integration harness path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../..")), nil
}

func pickFreePort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("pick free port: %w", err)
	}
	defer listener.Close()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return "", fmt.Errorf("split free port: %w", err)
	}
	return port, nil
}

func pickUnusedPort(used map[string]bool) (string, error) {
	for {
		port, err := pickFreePort()
		if err != nil {
			return "", err
		}
		if used[port] {
			continue
		}
		used[port] = true
		return port, nil
	}
}

func shortID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
}

func loadLiveSearchConfig(runID string) (*liveSearchConfig, error) {
	if !envTruthy(os.Getenv(liveSearchEnvFlag)) {
		return nil, nil
	}

	required := map[string]string{
		integrationTurbopufferAPIKeyEnv: strings.TrimSpace(os.Getenv(integrationTurbopufferAPIKeyEnv)),
		integrationTurbopufferRegionEnv: strings.TrimSpace(os.Getenv(integrationTurbopufferRegionEnv)),
		integrationTurbopufferPrefixEnv: strings.TrimSpace(os.Getenv(integrationTurbopufferPrefixEnv)),
		integrationModalAPIKeyEnv:       strings.TrimSpace(os.Getenv(integrationModalAPIKeyEnv)),
		integrationModalServerURLEnv:    strings.TrimSpace(os.Getenv(integrationModalServerURLEnv)),
	}

	missing := make([]string, 0)
	for key, value := range required {
		if value == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s requires %s", liveSearchEnvFlag, strings.Join(missing, ", "))
	}

	return &liveSearchConfig{
		TurbopufferAPIKey: required[integrationTurbopufferAPIKeyEnv],
		TurbopufferRegion: required[integrationTurbopufferRegionEnv],
		NamespacePrefix:   fmt.Sprintf("%s-it-%s", required[integrationTurbopufferPrefixEnv], runID),
		ModalServerAPIKey: required[integrationModalAPIKeyEnv],
		ModalEmbeddingURL: required[integrationModalServerURLEnv],
	}, nil
}

func envTruthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (s *composeStack) cleanupLiveSearchNamespaces(ctx context.Context) error {
	if s == nil || s.liveSearch == nil {
		return nil
	}

	client := tpuf.NewClient(
		option.WithAPIKey(s.liveSearch.TurbopufferAPIKey),
		option.WithRegion(s.liveSearch.TurbopufferRegion),
	)
	pager := client.NamespacesAutoPaging(ctx, tpuf.NamespacesParams{
		Prefix: tpuf.String(s.liveSearch.NamespacePrefix),
	})

	errs := make([]error, 0)
	for pager.Next() {
		namespace := pager.Current()
		ns := client.Namespace(namespace.ID)
		if _, err := ns.DeleteAll(ctx, tpuf.NamespaceDeleteAllParams{}); err != nil {
			errs = append(errs, fmt.Errorf("delete turbopuffer namespace %s: %w", namespace.ID, err))
		}
	}
	if err := pager.Err(); err != nil {
		errs = append(errs, fmt.Errorf("list turbopuffer namespaces with prefix %s: %w", s.liveSearch.NamespacePrefix, err))
	}
	return errors.Join(errs...)
}

func waitForPostgres(databaseURL string, timeout time.Duration) (*pgxpool.Pool, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		pool, err := pgxpool.New(ctx, databaseURL)
		cancel()
		if err == nil {
			pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = pool.Ping(pingCtx)
			pingCancel()
			if err == nil {
				return pool, nil
			}
			pool.Close()
			lastErr = err
		} else {
			lastErr = err
		}
		time.Sleep(pollInterval)
	}
	return nil, fmt.Errorf("postgres did not become ready: %w", lastErr)
}

func waitForHTTP(target string, timeout time.Duration) error {
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(target)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("service did not become healthy: %w", lastErr)
}

func newWorkflowHarness(t *testing.T) *workflowHarness {
	t.Helper()

	protector, err := teracrypto.NewStringProtector(context.Background(), teracrypto.Options{
		EnvKey: testEncryptionKey,
	})
	if err != nil {
		t.Fatalf("build string protector: %v", err)
	}

	clientTimeout := 15 * time.Second
	if testStack != nil && testStack.liveSearch != nil {
		clientTimeout = 60 * time.Second
	}

	return &workflowHarness{
		t:         t,
		pool:      testStack.pool,
		client:    &http.Client{Timeout: clientTimeout},
		baseURL:   testStack.baseURL,
		namespace: scopedSuffix(t.Name()),
		protector: protector,
		stack:     testStack,
	}
}

func (h *workflowHarness) requireLiveSearch(t *testing.T) {
	t.Helper()

	if h.stack == nil || h.stack.liveSearch == nil {
		t.Skip("live search integration disabled; run with INTEGRATION_LIVE_SEARCH=1 and search env configured")
	}
}

func scopedSuffix(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			if b.Len() == 0 || strings.HasSuffix(b.String(), "-") {
				continue
			}
			b.WriteByte('-')
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "it"
	}
	if len(base) > 32 {
		base = base[:32]
	}
	return base + "-" + shortID()[:6]
}

func (h *workflowHarness) scopedEmail(email string) string {
	local, domain, ok := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	if !ok {
		return email
	}
	return local + "+" + h.namespace + "@" + domain
}

func (h *workflowHarness) uniqueSlug(base string) string {
	slug := strings.ToLower(strings.TrimSpace(base))
	if slug == "" {
		slug = "it"
	}
	slug = slug + "-" + h.namespace
	if len(slug) > 63 {
		slug = slug[:63]
	}
	return strings.Trim(slug, "-")
}

func (h *workflowHarness) loginUser(t *testing.T, email string) actor {
	t.Helper()

	normalized := h.scopedEmail(email)
	code := fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	now := time.Now().UTC()
	if _, err := h.pool.Exec(
		context.Background(),
		`insert into email_login_challenges (id, email, code_hash, expires_at, created_at)
		values ($1,$2,$3,$4,$5)`,
		uuid.New(),
		normalized,
		teracrypto.SHA256Hex(code),
		now.Add(10*time.Minute),
		now,
	); err != nil {
		t.Fatalf("insert email login challenge for %s: %v", normalized, err)
	}

	response := mustJSON[api.AuthResponse](
		t,
		h,
		http.MethodPost,
		"/auth/email/verify",
		"",
		api.VerifyEmailLoginRequest{Email: normalized, Code: code},
		http.StatusOK,
	)
	return actor{
		User:  response.User,
		Token: response.Session.Token,
		Email: normalized,
	}
}

func (h *workflowHarness) waitForExternalEvent(t *testing.T, token string, path string) api.ExternalEvent {
	t.Helper()

	return h.waitForExternalEventMatch(t, token, path, func(api.ExternalEvent) bool { return true })
}

func (h *workflowHarness) waitForExternalEventMatch(t *testing.T, token string, path string, match func(api.ExternalEvent) bool) api.ExternalEvent {
	t.Helper()

	var response api.CollectionResponse[api.ExternalEvent]
	var matched api.ExternalEvent
	waitForCondition(t, "external event "+path, eventualTimeout, func() (bool, error) {
		response = mustJSON[api.CollectionResponse[api.ExternalEvent]](
			t,
			h,
			http.MethodGet,
			path,
			token,
			nil,
			http.StatusOK,
		)
		for _, item := range response.Items {
			if match(item) {
				matched = item
				return true, nil
			}
		}
		return false, nil
	})
	return matched
}

func (h *workflowHarness) search(t *testing.T, token string, request api.SearchRequest) api.SearchResponse {
	t.Helper()
	return mustJSON[api.SearchResponse](t, h, http.MethodPost, "/search", token, request, http.StatusOK)
}

func (h *workflowHarness) waitForSearchHit(t *testing.T, token string, request api.SearchRequest, match func(api.SearchHit) bool) api.SearchHit {
	t.Helper()

	var matched api.SearchHit
	waitForCondition(t, "search query "+strings.TrimSpace(request.Query), searchWaitTimeout, func() (bool, error) {
		response := h.search(t, token, request)
		for _, item := range response.Items {
			if match(item) {
				matched = item
				return true, nil
			}
		}
		return false, nil
	})
	return matched
}

func (h *workflowHarness) waitForProjectionFailureCount(t *testing.T, internalEventID uuid.UUID, want int) {
	t.Helper()

	waitForCondition(t, fmt.Sprintf("projection failure count for internal event %s", internalEventID), eventualTimeout, func() (bool, error) {
		var count int
		err := h.pool.QueryRow(
			context.Background(),
			`select count(*)::int
			from external_event_projection_failures
			where internal_event_id = $1`,
			internalEventID,
		).Scan(&count)
		return count == want, err
	})
}

func (h *workflowHarness) waitForProjectedExternalEventCount(t *testing.T, internalEventID uuid.UUID, want int) {
	t.Helper()

	waitForCondition(t, fmt.Sprintf("external event projection count for internal event %s", internalEventID), eventualTimeout, func() (bool, error) {
		var count int
		err := h.pool.QueryRow(
			context.Background(),
			`select count(*)::int
			from external_events
			where source_internal_event_id = $1`,
			internalEventID,
		).Scan(&count)
		return count == want, err
	})
}

func (h *workflowHarness) mustScaleService(t *testing.T, service string, count int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := h.stack.ScaleService(ctx, service, count); err != nil {
		t.Fatalf("scale %s to %d: %v", service, count, err)
	}
}

func waitForCondition(t *testing.T, description string, timeout time.Duration, fn func() (bool, error)) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ok, err := fn()
		if err == nil && ok {
			return
		}
		if err != nil {
			lastErr = err
		}
		time.Sleep(pollInterval)
	}
	if lastErr != nil {
		t.Fatalf("timed out waiting for %s: %v", description, lastErr)
	}
	t.Fatalf("timed out waiting for %s", description)
}

func (h *workflowHarness) mustNoContent(t *testing.T, method string, path string, token string, body any, wantStatus int) {
	t.Helper()
	response := h.mustRequest(t, method, path, token, body, wantStatus)
	response.Body.Close()
}

func (h *workflowHarness) mustRequest(t *testing.T, method string, path string, token string, body any, wantStatus int) *http.Response {
	t.Helper()

	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body for %s %s: %v", method, path, err)
		}
		payload = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, h.baseURL+path, payload)
	if err != nil {
		t.Fatalf("build %s %s request: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("execute %s %s request: %v", method, path, err)
	}
	if response.StatusCode != wantStatus {
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("%s %s returned %d, want %d: %s", method, path, response.StatusCode, wantStatus, strings.TrimSpace(string(raw)))
	}
	return response
}

func (h *workflowHarness) mustCountInternalEvents(t *testing.T, eventType string, workspaceID string, userID string) int {
	t.Helper()

	var count int
	if err := h.pool.QueryRow(
		context.Background(),
		`select count(*)::int
		from internal_events
		where event_type = $1
		  and coalesce(payload->>'workspace_id', '') = $2
		  and coalesce(payload->>'user_id', '') = $3`,
		eventType,
		workspaceID,
		userID,
	).Scan(&count); err != nil {
		t.Fatalf("count internal events: %v", err)
	}
	return count
}

func (h *workflowHarness) mustLastReadMessageID(t *testing.T, conversationID string, userID string) string {
	t.Helper()

	var lastReadMessageID uuid.UUID
	if err := h.pool.QueryRow(
		context.Background(),
		`select last_read_message_id
		from conversation_reads
		where conversation_id = $1 and user_id = $2`,
		conversationID,
		userID,
	).Scan(&lastReadMessageID); err != nil {
		t.Fatalf("load read state for conversation %s user %s: %v", conversationID, userID, err)
	}
	return lastReadMessageID.String()
}

func (h *workflowHarness) mustEncryptedSubscriptionSecret(t *testing.T, subscriptionID string) string {
	t.Helper()

	var encrypted string
	if err := h.pool.QueryRow(
		context.Background(),
		`select encrypted_secret
		from event_subscriptions
		where id = $1`,
		subscriptionID,
	).Scan(&encrypted); err != nil {
		t.Fatalf("load encrypted secret for subscription %s: %v", subscriptionID, err)
	}
	return encrypted
}

func newWebhookRecorder(t *testing.T) *webhookRecorder {
	return newWebhookRecorderWithDelay(t, 0)
}

func newWebhookRecorderWithDelay(t *testing.T, delay time.Duration) *webhookRecorder {
	t.Helper()

	recorder := &webhookRecorder{
		adminURL:     testStack.webhookRecorderURL,
		containerURL: "http://webhook-recorder:8090",
		client:       &http.Client{Timeout: 5 * time.Second},
	}
	recorder.reset(t, delay)
	return recorder
}

func (r *webhookRecorder) ContainerURL() string {
	return r.containerURL
}

func (r *webhookRecorder) waitForRecord(t *testing.T) webhookRecord {
	t.Helper()

	var record webhookRecord
	waitForCondition(t, "webhook delivery", eventualTimeout, func() (bool, error) {
		records, err := r.fetchRecords()
		if err != nil {
			return false, err
		}
		if len(records) == 0 {
			return false, nil
		}
		record = records[0]
		return true, nil
	})
	return record
}

func (r *webhookRecorder) recordCount() int {
	records, err := r.fetchRecords()
	if err != nil {
		return 0
	}
	return len(records)
}

func (r *webhookRecorder) reset(t *testing.T, delay time.Duration) {
	t.Helper()

	body, err := json.Marshal(map[string]int{"delay_ms": int(delay / time.Millisecond)})
	if err != nil {
		t.Fatalf("marshal webhook recorder reset: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, r.adminURL+"/_admin/reset", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build webhook recorder reset request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		t.Fatalf("reset webhook recorder: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("reset webhook recorder returned %d: %s", response.StatusCode, strings.TrimSpace(string(payload)))
	}
}

func (r *webhookRecorder) fetchRecords() ([]webhookRecord, error) {
	response, err := r.client.Get(r.adminURL + "/_admin/records")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("webhook recorder returned %d: %s", response.StatusCode, strings.TrimSpace(string(payload)))
	}

	var payload webhookRecorderResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}

	records := make([]webhookRecord, 0, len(payload.Records))
	for _, item := range payload.Records {
		body, err := base64.StdEncoding.DecodeString(item.BodyBase64)
		if err != nil {
			return nil, err
		}
		records = append(records, webhookRecord{
			Body:      body,
			Headers:   http.Header(item.Headers),
			Signature: item.Signature,
		})
	}
	return records, nil
}

func countNonEmptyLines(value string) int {
	count := 0
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func mustJSON[T any](t *testing.T, h *workflowHarness, method string, path string, token string, body any, wantStatus int) T {
	t.Helper()
	response := h.mustRequest(t, method, path, token, body, wantStatus)
	defer response.Body.Close()

	var out T
	if response.StatusCode == http.StatusNoContent {
		return out
	}
	if err := json.NewDecoder(response.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s %s response: %v", method, path, err)
	}
	return out
}
