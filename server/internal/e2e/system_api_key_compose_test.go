package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	openapi "github.com/suhjohn/teraslack/internal/api"
	"github.com/suhjohn/teraslack/internal/domain"
)

func TestComposeE2E_SystemAPIKeyCanReachProtectedRoutes(t *testing.T) {
	_, _, httpClient, baseURL, owner, ownerToken := setupComposeE2EHTTP(t)

	member := createUserViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateUserParams{
		Name:          uniqueName("system-member"),
		Email:         uniqueEmail("system-member"),
		PrincipalType: domain.PrincipalTypeHuman,
		AccountType:   domain.AccountTypeMember,
	})
	agent := createUserViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateUserParams{
		Name:          uniqueName("system-agent"),
		Email:         uniqueEmail("system-agent"),
		PrincipalType: domain.PrincipalTypeAgent,
		OwnerID:       owner.ID,
		IsBot:         true,
	})

	managedKey, _ := createAPIKeyViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateAPIKeyParams{
		Name:        "Managed Route Key",
		WorkspaceID: owner.WorkspaceID,
		UserID:      agent.ID,
		CreatedBy:   owner.ID,
		Permissions: []string{domain.PermissionMessagesRead, domain.PermissionMessagesWrite},
	})
	_, systemKey := createAPIKeyViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateAPIKeyParams{
		Name:        "System Route Key",
		WorkspaceID: owner.WorkspaceID,
		CreatedBy:   owner.ID,
		Permissions: []string{"*"},
	})

	channel := createConversationViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateConversationParams{
		WorkspaceID: owner.WorkspaceID,
		Name:        uniqueName("system-routes"),
		Type:        domain.ConversationTypePublicChannel,
		CreatorID:   owner.ID,
		UserIDs:     []string{member.ID, agent.ID},
	})
	rootMessage := postMessageViaHTTP(t, httpClient, baseURL, ownerToken, domain.PostMessageParams{
		ChannelID: channel.ID,
		UserID:    owner.ID,
		Text:      "system route coverage seed",
	})
	addReactionViaHTTP(t, httpClient, baseURL, ownerToken, channel.ID, rootMessage.TS, "eyes", owner.ID)
	addPinViaHTTP(t, httpClient, baseURL, ownerToken, channel.ID, rootMessage.TS)
	bookmark := createBookmarkViaHTTP(t, httpClient, baseURL, ownerToken, domain.CreateBookmarkParams{
		ChannelID: channel.ID,
		Title:     "System Route Bookmark",
		Type:      "link",
		Link:      "https://example.com/system-routes",
		CreatedBy: owner.ID,
	})

	usergroup := createUsergroupViaHTTP(t, httpClient, baseURL, ownerToken, map[string]any{
		"name":        uniqueName("system-ug"),
		"handle":      uniqueName("system-ug"),
		"description": "system route coverage",
		"users":       []string{owner.ID, member.ID},
	})
	subscription := createEventSubscriptionViaHTTP(t, httpClient, baseURL, ownerToken, map[string]any{
		"workspace_id":  owner.WorkspaceID,
		"url":           "https://example.com/webhook/system-routes",
		"type":          domain.EventTypeConversationMessageCreated,
		"resource_type": domain.ResourceTypeConversation,
		"resource_id":   channel.ID,
		"secret":        "system-route-secret",
	})
	externalAccess := createExternalAccessViaHTTP(t, httpClient, baseURL, ownerToken, map[string]any{
		"host_workspace_id": owner.WorkspaceID,
		"principal_id":      agent.ID,
		"principal_type":    domain.PrincipalTypeAgent,
		"home_workspace_id": owner.WorkspaceID,
		"access_mode":       domain.ExternalPrincipalAccessModeShared,
		"allowed_capabilities": []string{
			domain.PermissionMessagesRead,
		},
		"conversation_ids": []string{channel.ID},
	})

	spec, err := openapi.GetSwagger()
	if err != nil {
		t.Fatalf("load swagger: %v", err)
	}

	fixtures := systemRouteFixtures{
		workspaceID:        owner.WorkspaceID,
		ownerID:            owner.ID,
		memberID:           member.ID,
		agentID:            agent.ID,
		channelID:          channel.ID,
		messageTS:          rootMessage.TS,
		bookmarkID:         bookmark.ID,
		apiKeyID:           managedKey.ID,
		usergroupID:        usergroup.ID,
		subscriptionID:     subscription.ID,
		externalAccessID:   externalAccess.ID,
		fakeExternalWSID:   "EW_DOES_NOT_EXIST",
		fakeFileID:         "F_DOES_NOT_EXIST",
		fakeUploadFileID:   "F_UPLOAD_DOES_NOT_EXIST",
		fakeBookmarkID:     "B_DOES_NOT_EXIST",
		fakeMessageTS:      "9999.999999",
		fakeConversationID: "C_DOES_NOT_EXIST",
	}

	cases := collectSystemRouteCases(t, spec, fixtures)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := doRawRequest(t, httpClient, tc.method, tc.url, systemKey, tc.body)
			if status == http.StatusUnauthorized || status == http.StatusForbidden || status >= 500 {
				t.Fatalf("%s %s returned status=%d body=%s", tc.method, tc.url, status, body)
			}
		})
	}
}

type systemRouteFixtures struct {
	workspaceID        string
	ownerID            string
	memberID           string
	agentID            string
	channelID          string
	messageTS          string
	bookmarkID         string
	apiKeyID           string
	usergroupID        string
	subscriptionID     string
	externalAccessID   string
	fakeExternalWSID   string
	fakeFileID         string
	fakeUploadFileID   string
	fakeBookmarkID     string
	fakeMessageTS      string
	fakeConversationID string
}

type systemRouteCase struct {
	name   string
	method string
	url    string
	body   any
}

func collectSystemRouteCases(t *testing.T, spec *openapi3.T, fx systemRouteFixtures) []systemRouteCase {
	t.Helper()

	var cases []systemRouteCase
	for path, item := range spec.Paths.Map() {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete} {
			if !hasOperation(item, method) || !isProtectedRoute(path) {
				continue
			}
			cases = append(cases, systemRouteCase{
				name:   method + " " + path,
				method: method,
				url:    buildSystemRouteURL("http://placeholder", path, fx),
				body:   buildSystemRouteBody(method, path, fx),
			})
		}
	}

	cases = append(cases, systemRouteCase{
		name:   http.MethodPost + " /workspaces/{id}/invites",
		method: http.MethodPost,
		url:    buildSystemRouteURL("http://placeholder", "/workspaces/{id}/invites", fx),
		body: map[string]any{
			"email": uniqueEmail("system-invite"),
		},
	})

	sort.Slice(cases, func(i, j int) bool {
		if cases[i].url != cases[j].url {
			return cases[i].url < cases[j].url
		}
		return methodWeight(cases[i].method) < methodWeight(cases[j].method)
	})

	for i := range cases {
		cases[i].url = strings.Replace(cases[i].url, "http://placeholder", "", 1)
	}
	return cases
}

func hasOperation(item *openapi3.PathItem, method string) bool {
	if item == nil {
		return false
	}
	switch method {
	case http.MethodGet:
		return item.Get != nil
	case http.MethodPost:
		return item.Post != nil
	case http.MethodPatch:
		return item.Patch != nil
	case http.MethodPut:
		return item.Put != nil
	case http.MethodDelete:
		return item.Delete != nil
	default:
		return false
	}
}

func isProtectedRoute(path string) bool {
	if path == "/healthz" || path == "/openapi.json" || path == "/openapi.yaml" {
		return false
	}
	return !strings.HasPrefix(path, "/auth/oauth/") &&
		!strings.HasPrefix(path, "/cli/install/")
}

func buildSystemRouteURL(baseURL, path string, fx systemRouteFixtures) string {
	urlPath := path
	switch {
	case strings.HasPrefix(path, "/workspaces/{id}"):
		urlPath = strings.ReplaceAll(urlPath, "{id}", fx.workspaceID)
	case strings.HasPrefix(path, "/users/{id}"):
		urlPath = strings.ReplaceAll(urlPath, "{id}", fx.memberID)
	case strings.HasPrefix(path, "/usergroups/{id}"):
		urlPath = strings.ReplaceAll(urlPath, "{id}", fx.usergroupID)
	case strings.HasPrefix(path, "/api-keys/{id}"):
		urlPath = strings.ReplaceAll(urlPath, "{id}", fx.apiKeyID)
	case strings.HasPrefix(path, "/event-subscriptions/{id}"):
		urlPath = strings.ReplaceAll(urlPath, "{id}", fx.subscriptionID)
	case strings.HasPrefix(path, "/external-principal-access/{id}"):
		urlPath = strings.ReplaceAll(urlPath, "{id}", fx.externalAccessID)
	case strings.HasPrefix(path, "/file-uploads/{id}"):
		urlPath = strings.ReplaceAll(urlPath, "{id}", fx.fakeUploadFileID)
	case strings.HasPrefix(path, "/files/{id}"):
		urlPath = strings.ReplaceAll(urlPath, "{id}", fx.fakeFileID)
	case strings.HasPrefix(path, "/conversations/{conversation_id}"):
		urlPath = strings.ReplaceAll(urlPath, "{conversation_id}", fx.channelID)
	case strings.HasPrefix(path, "/conversations/{id}"):
		urlPath = strings.ReplaceAll(urlPath, "{id}", fx.channelID)
	case strings.HasPrefix(path, "/messages/{conversation_id}"):
		urlPath = strings.ReplaceAll(urlPath, "{conversation_id}", fx.channelID)
	}

	replacements := map[string]string{
		"{bookmark_id}":           fx.bookmarkID,
		"{user_id}":               fx.memberID,
		"{message_ts}":            fx.fakeMessageTS,
		"{reaction_name}":         "eyes",
		"{external_workspace_id}": fx.fakeExternalWSID,
	}
	if strings.Contains(path, "/reactions") || strings.HasSuffix(path, "/pins/{message_ts}") {
		replacements["{message_ts}"] = fx.messageTS
	}
	for token, value := range replacements {
		urlPath = strings.ReplaceAll(urlPath, token, value)
	}

	switch path {
	case "/messages":
		return baseURL + urlPath + "?conversation_id=" + fx.channelID
	case "/events":
		return baseURL + urlPath + "?limit=1"
	case "/files":
		return baseURL + urlPath + "?conversation_id=" + fx.channelID
	default:
		return baseURL + urlPath
	}
}

func buildSystemRouteBody(method, path string, fx systemRouteFixtures) any {
	switch method + " " + path {
	case http.MethodPost + " /api-keys":
		return map[string]any{
			"name":         "route-created-key",
			"workspace_id": fx.workspaceID,
			"user_id":      fx.agentID,
			"permissions":  []string{"*"},
		}
	case http.MethodPatch + " /api-keys/{id}":
		return map[string]any{"name": "route-key-updated"}
	case http.MethodPost + " /api-keys/{id}/rotations":
		return map[string]any{}
	case http.MethodPost + " /auth/sessions/current/workspace":
		return map[string]any{"workspace_id": fx.workspaceID}
	case http.MethodPost + " /conversations":
		return map[string]any{
			"workspace_id": fx.workspaceID,
			"name":         uniqueName("route-conversation"),
			"type":         domain.ConversationTypePublicChannel,
			"creator_id":   fx.ownerID,
		}
	case http.MethodPatch + " /conversations/{id}":
		return map[string]any{"name": "route-channel-updated"}
	case http.MethodPost + " /conversations/{id}/bookmarks":
		return map[string]any{
			"channel_id": fx.channelID,
			"title":      "route bookmark",
			"type":       "link",
			"link":       "https://example.com/route-bookmark",
			"created_by": fx.ownerID,
		}
	case http.MethodPatch + " /conversations/{conversation_id}/bookmarks/{bookmark_id}":
		return map[string]any{"title": "route bookmark updated"}
	case http.MethodPut + " /conversations/{id}/managers":
		return map[string]any{"user_ids": []string{fx.ownerID}}
	case http.MethodPost + " /conversations/{id}/members":
		return map[string]any{"user_ids": []string{fx.memberID}}
	case http.MethodPut + " /conversations/{id}/posting-policy":
		return map[string]any{"policy_type": domain.ConversationPostingPolicyEveryone}
	case http.MethodPut + " /conversations/{id}/read-state":
		return map[string]any{"last_read_ts": fx.messageTS}
	case http.MethodPost + " /event-subscriptions":
		return map[string]any{
			"workspace_id":  ownerWorkspace(fx),
			"url":           "https://example.com/system-route-create",
			"type":          domain.EventTypeConversationMessageCreated,
			"resource_type": domain.ResourceTypeConversation,
			"resource_id":   fx.channelID,
			"secret":        "route-secret",
		}
	case http.MethodPatch + " /event-subscriptions/{id}":
		return map[string]any{"enabled": true}
	case http.MethodPost + " /external-principal-access":
		return map[string]any{
			"host_workspace_id": ownerWorkspace(fx),
			"principal_id":      fx.agentID,
			"principal_type":    domain.PrincipalTypeAgent,
			"home_workspace_id": ownerWorkspace(fx),
			"access_mode":       domain.ExternalPrincipalAccessModeShared,
			"allowed_capabilities": []string{
				domain.PermissionMessagesRead,
			},
			"conversation_ids": []string{fx.channelID},
		}
	case http.MethodPatch + " /external-principal-access/{id}":
		return map[string]any{"allowed_capabilities": []string{domain.PermissionMessagesRead}}
	case http.MethodPost + " /file-uploads":
		return map[string]any{"filename": "route-upload.txt", "length": 3}
	case http.MethodPost + " /file-uploads/{id}/complete":
		return map[string]any{"title": "route upload complete", "channel_id": fx.channelID}
	case http.MethodPost + " /files":
		return map[string]any{
			"title":        "route remote file",
			"external_url": "https://example.com/route-file.txt",
			"filetype":     "txt",
		}
	case http.MethodPost + " /files/{id}/shares":
		return map[string]any{"channels": []string{fx.channelID}}
	case http.MethodPost + " /messages":
		return map[string]any{
			"channel_id": fx.channelID,
			"user_id":    fx.ownerID,
			"text":       "route message",
		}
	case http.MethodPatch + " /messages/{conversation_id}/{message_ts}":
		return map[string]any{"text": "route message updated"}
	case http.MethodPost + " /messages/{conversation_id}/{message_ts}/reactions":
		return map[string]any{"name": "eyes"}
	case http.MethodPost + " /search":
		return map[string]any{"workspace_id": fx.workspaceID, "query": "route"}
	case http.MethodPost + " /usergroups":
		return map[string]any{
			"workspace_id": fx.workspaceID,
			"name":         uniqueName("route-usergroup"),
			"handle":       uniqueName("route-usergroup"),
			"description":  "route usergroup",
			"users":        []string{fx.ownerID, fx.memberID},
		}
	case http.MethodPatch + " /usergroups/{id}":
		return map[string]any{"name": "route-usergroup-updated"}
	case http.MethodPut + " /usergroups/{id}/members":
		return map[string]any{"users": []string{fx.ownerID, fx.memberID}}
	case http.MethodPost + " /users":
		return map[string]any{
			"workspace_id":   fx.workspaceID,
			"name":           uniqueName("route-user"),
			"email":          uniqueEmail("route-user"),
			"principal_type": domain.PrincipalTypeHuman,
			"account_type":   domain.AccountTypeMember,
		}
	case http.MethodPatch + " /users/{id}":
		return map[string]any{"display_name": "route-display-name"}
	case http.MethodPut + " /users/{id}/roles":
		return map[string]any{"delegated_roles": []string{string(domain.DelegatedRoleRolesAdmin)}}
	case http.MethodPost + " /workspaces":
		return map[string]any{}
	case http.MethodPatch + " /workspaces/{id}":
		return map[string]any{"name": "route-workspace-updated"}
	case http.MethodPost + " /workspaces/{id}/primary-admin":
		return map[string]any{"user_id": fx.memberID}
	case http.MethodPost + " /workspaces/{id}/invites":
		return map[string]any{"email": uniqueEmail("route-invite")}
	}

	if method == http.MethodPost || method == http.MethodPatch || method == http.MethodPut {
		return map[string]any{}
	}
	return nil
}

func createUsergroupViaHTTP(t *testing.T, httpClient *http.Client, baseURL, auth string, body map[string]any) domain.Usergroup {
	t.Helper()
	var resp domain.Usergroup
	doJSON(t, httpClient, http.MethodPost, baseURL+"/usergroups", auth, body, &resp)
	if resp.ID == "" {
		t.Fatalf("create usergroup response = %+v", resp)
	}
	return resp
}

func createExternalAccessViaHTTP(t *testing.T, httpClient *http.Client, baseURL, auth string, body map[string]any) domain.ExternalPrincipalAccess {
	t.Helper()
	var resp domain.ExternalPrincipalAccess
	doJSON(t, httpClient, http.MethodPost, baseURL+"/external-principal-access", auth, body, &resp)
	if resp.ID == "" {
		t.Fatalf("create external principal access response = %+v", resp)
	}
	return resp
}

func doRawRequest(t *testing.T, httpClient *http.Client, method, urlPath, auth string, body any) (int, string) {
	t.Helper()

	fullURL := "http://localhost"
	if strings.HasPrefix(urlPath, "http://") || strings.HasPrefix(urlPath, "https://") {
		fullURL = urlPath
	} else {
		fullURL = strings.TrimRight(e2eBaseURL(), "/") + urlPath
	}

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body for %s %s: %v", method, fullURL, err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, fullURL, bodyReader)
	if err != nil {
		t.Fatalf("new request for %s %s: %v", method, fullURL, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+auth)

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, fullURL, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body for %s %s: %v", method, fullURL, err)
	}
	return resp.StatusCode, string(data)
}

func methodWeight(method string) int {
	switch method {
	case http.MethodGet:
		return 0
	case http.MethodPost:
		return 1
	case http.MethodPatch:
		return 2
	case http.MethodPut:
		return 3
	case http.MethodDelete:
		return 4
	default:
		return 5
	}
}

func ownerWorkspace(fx systemRouteFixtures) string {
	return fx.workspaceID
}
