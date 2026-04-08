//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/johnsuh/teraslack/server/internal/api"
	teracrypto "github.com/johnsuh/teraslack/server/internal/crypto"
	"github.com/johnsuh/teraslack/server/internal/eventsourcing"
)

func TestSPECWorkflows_BootstrapAuthenticatedStateAndWorkspaceCreation(t *testing.T) {
	h := newWorkflowHarness(t)
	alpha := h.loginUser(t, "alpha@example.com")

	me := mustJSON[api.MeResponse](t, h, http.MethodGet, "/me", alpha.Token, nil, http.StatusOK)
	if me.User.ID != alpha.User.ID {
		t.Fatalf("GET /me returned user %s, want %s", me.User.ID, alpha.User.ID)
	}
	if len(me.Workspaces) != 0 {
		t.Fatalf("GET /me returned %d workspaces before creation, want 0", len(me.Workspaces))
	}

	globalConversation := mustJSON[api.Conversation](
		t,
		h,
		http.MethodPost,
		"/conversations",
		alpha.Token,
		nil,
		http.StatusCreated,
	)
	if globalConversation.WorkspaceID != nil {
		t.Fatalf("global conversation unexpectedly had workspace_id %v", *globalConversation.WorkspaceID)
	}
	if globalConversation.AccessPolicy != "members" {
		t.Fatalf("default global conversation had access_policy %q, want members", globalConversation.AccessPolicy)
	}
	if globalConversation.ShareLink == nil {
		t.Fatalf("default global conversation did not return a share link")
	}

	workspace := mustJSON[api.Workspace](
		t,
		h,
		http.MethodPost,
		"/workspaces",
		alpha.Token,
		api.CreateWorkspaceRequest{Name: "Acme", Slug: h.uniqueSlug("acme")},
		http.StatusCreated,
	)

	me = mustJSON[api.MeResponse](t, h, http.MethodGet, "/me", alpha.Token, nil, http.StatusOK)
	if len(me.Workspaces) != 1 {
		t.Fatalf("GET /me returned %d workspaces after creation, want 1", len(me.Workspaces))
	}
	if me.Workspaces[0].WorkspaceID != workspace.ID {
		t.Fatalf("workspace summary returned %s, want %s", me.Workspaces[0].WorkspaceID, workspace.ID)
	}

	globalList := mustJSON[api.CollectionResponse[api.Conversation]](t, h, http.MethodGet, "/conversations", alpha.Token, nil, http.StatusOK)
	if len(globalList.Items) != 1 {
		t.Fatalf("GET /conversations returned %d global conversations, want 1", len(globalList.Items))
	}
	if globalList.Items[0].ID != globalConversation.ID {
		t.Fatalf("global conversation list returned %s, want %s", globalList.Items[0].ID, globalConversation.ID)
	}

	workspaceList := mustJSON[api.CollectionResponse[api.Conversation]](
		t,
		h,
		http.MethodGet,
		"/conversations?workspace_id="+url.QueryEscape(workspace.ID),
		alpha.Token,
		nil,
		http.StatusOK,
	)
	if len(workspaceList.Items) != 1 {
		t.Fatalf("GET /conversations?workspace_id returned %d conversations, want 1", len(workspaceList.Items))
	}
	general := workspaceList.Items[0]
	if general.AccessPolicy != "workspace" {
		t.Fatalf("default workspace conversation had access_policy %q, want workspace", general.AccessPolicy)
	}
	if general.Title == nil || *general.Title != "general" {
		t.Fatalf("default workspace conversation title = %v, want general", general.Title)
	}
}

func TestSPECWorkflows_GlobalCanonicalDMIsReused(t *testing.T) {
	h := newWorkflowHarness(t)
	alpha := h.loginUser(t, "alpha@example.com")
	beta := h.loginUser(t, "beta@example.com")

	dm := mustJSON[api.Conversation](
		t,
		h,
		http.MethodPost,
		"/conversations",
		alpha.Token,
		api.CreateConversationRequest{
			WorkspaceID:        nil,
			AccessPolicy:       "members",
			ParticipantUserIDs: []string{beta.User.ID},
		},
		http.StatusCreated,
	)
	if dm.WorkspaceID != nil {
		t.Fatalf("DM workspace_id = %v, want nil", *dm.WorkspaceID)
	}

	existingDM := mustJSON[api.Conversation](
		t,
		h,
		http.MethodPost,
		"/conversations",
		alpha.Token,
		api.CreateConversationRequest{
			WorkspaceID:        nil,
			AccessPolicy:       "members",
			ParticipantUserIDs: []string{beta.User.ID},
		},
		http.StatusOK,
	)
	if existingDM.ID != dm.ID {
		t.Fatalf("canonical DM returned %s, want %s", existingDM.ID, dm.ID)
	}

	message := mustJSON[api.Message](
		t,
		h,
		http.MethodPost,
		"/conversations/"+dm.ID+"/messages",
		alpha.Token,
		api.CreateMessageRequest{BodyText: "hello beta"},
		http.StatusCreated,
	)
	if message.AuthorUserID != alpha.User.ID {
		t.Fatalf("message author = %s, want %s", message.AuthorUserID, alpha.User.ID)
	}

	messages := mustJSON[api.CollectionResponse[api.Message]](
		t,
		h,
		http.MethodGet,
		"/conversations/"+dm.ID+"/messages",
		beta.Token,
		nil,
		http.StatusOK,
	)
	if len(messages.Items) != 1 || messages.Items[0].ID != message.ID {
		t.Fatalf("message list returned %+v, want message %s", messages.Items, message.ID)
	}

	h.mustNoContent(
		t,
		http.MethodPut,
		"/conversations/"+dm.ID+"/read-state",
		beta.Token,
		api.UpdateReadStateRequest{LastReadMessageID: message.ID},
		http.StatusNoContent,
	)
	if got := h.mustLastReadMessageID(t, dm.ID, beta.User.ID); got != message.ID {
		t.Fatalf("stored read state = %s, want %s", got, message.ID)
	}

	event := h.waitForExternalEvent(
		t,
		alpha.Token,
		"/events?type=conversation.message.created&resource_type=conversation&resource_id="+url.QueryEscape(dm.ID),
	)
	if event.ResourceType != "conversation" || event.ResourceID != dm.ID {
		t.Fatalf("event resource = %s/%s, want conversation/%s", event.ResourceType, event.ResourceID, dm.ID)
	}
	if event.Payload["message_id"] != message.ID {
		t.Fatalf("event payload message_id = %v, want %s", event.Payload["message_id"], message.ID)
	}
}

func TestSPECWorkflows_GlobalConversationSupportsBackAndForthMessaging(t *testing.T) {
	h := newWorkflowHarness(t)
	alpha := h.loginUser(t, "alpha@example.com")
	beta := h.loginUser(t, "beta@example.com")

	conversation := mustJSON[api.Conversation](
		t,
		h,
		http.MethodPost,
		"/conversations",
		alpha.Token,
		api.CreateConversationRequest{
			WorkspaceID:        nil,
			AccessPolicy:       "members",
			ParticipantUserIDs: []string{beta.User.ID},
		},
		http.StatusCreated,
	)
	if conversation.WorkspaceID != nil {
		t.Fatalf("global conversation workspace_id = %v, want nil", *conversation.WorkspaceID)
	}
	if conversation.AccessPolicy != "members" {
		t.Fatalf("global conversation access_policy = %q, want members", conversation.AccessPolicy)
	}

	first := mustJSON[api.Message](
		t,
		h,
		http.MethodPost,
		"/conversations/"+conversation.ID+"/messages",
		alpha.Token,
		api.CreateMessageRequest{BodyText: "alpha says hello"},
		http.StatusCreated,
	)
	if first.AuthorUserID != alpha.User.ID {
		t.Fatalf("first message author = %s, want %s", first.AuthorUserID, alpha.User.ID)
	}

	second := mustJSON[api.Message](
		t,
		h,
		http.MethodPost,
		"/conversations/"+conversation.ID+"/messages",
		beta.Token,
		api.CreateMessageRequest{BodyText: "beta replies back"},
		http.StatusCreated,
	)
	if second.AuthorUserID != beta.User.ID {
		t.Fatalf("second message author = %s, want %s", second.AuthorUserID, beta.User.ID)
	}

	for _, actor := range []actor{alpha, beta} {
		conversations := mustJSON[api.CollectionResponse[api.Conversation]](
			t,
			h,
			http.MethodGet,
			"/conversations",
			actor.Token,
			nil,
			http.StatusOK,
		)
		found := false
		for _, item := range conversations.Items {
			if item.ID == conversation.ID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("conversation %s not visible to user %s in global conversation list", conversation.ID, actor.User.ID)
		}

		messages := mustJSON[api.CollectionResponse[api.Message]](
			t,
			h,
			http.MethodGet,
			"/conversations/"+conversation.ID+"/messages",
			actor.Token,
			nil,
			http.StatusOK,
		)
		if len(messages.Items) != 2 {
			t.Fatalf("message list for user %s returned %d items, want 2", actor.User.ID, len(messages.Items))
		}
		if messages.Items[0].ID != second.ID || messages.Items[1].ID != first.ID {
			t.Fatalf("message order for user %s = [%s, %s], want [%s, %s]", actor.User.ID, messages.Items[0].ID, messages.Items[1].ID, second.ID, first.ID)
		}
		if messages.Items[0].AuthorUserID != beta.User.ID || messages.Items[1].AuthorUserID != alpha.User.ID {
			t.Fatalf("message authors for user %s = [%s, %s], want [%s, %s]", actor.User.ID, messages.Items[0].AuthorUserID, messages.Items[1].AuthorUserID, beta.User.ID, alpha.User.ID)
		}
	}
}

func TestSPECWorkflows_SearchHybridIndexingAndACL(t *testing.T) {
	h := newWorkflowHarness(t)
	h.requireLiveSearch(t)
	h.mustScaleService(t, "indexer", 2)

	alpha := h.loginUser(t, "alpha@example.com")
	beta := h.loginUser(t, "beta@example.com")
	gamma := h.loginUser(t, "gamma@example.com")

	token := strings.ReplaceAll(h.namespace, "-", "")
	workspaceQuery := "workspace" + token
	conversationQuery := "conversation" + token
	userQuery := "user" + token
	workspaceMessageQuery := "workmsg" + token
	privateMessageQuery := "privmsg" + token
	globalMessageQuery := "globmsg" + token

	updatedBeta := mustJSON[api.User](
		t,
		h,
		http.MethodPatch,
		"/me/profile",
		beta.Token,
		api.UpdateProfileRequest{
			Handle:      stringPtr("beta" + token),
			DisplayName: stringPtr("Beta " + userQuery),
		},
		http.StatusOK,
	)

	workspace := mustJSON[api.Workspace](
		t,
		h,
		http.MethodPost,
		"/workspaces",
		alpha.Token,
		api.CreateWorkspaceRequest{
			Name: "Workspace " + workspaceQuery,
			Slug: h.uniqueSlug("search"),
		},
		http.StatusCreated,
	)

	invite := mustJSON[api.CreateWorkspaceInviteResponse](
		t,
		h,
		http.MethodPost,
		"/workspaces/"+workspace.ID+"/invites",
		alpha.Token,
		api.CreateWorkspaceInviteRequest{Email: &beta.Email},
		http.StatusCreated,
	)
	mustJSON[api.WorkspaceMember](
		t,
		h,
		http.MethodPost,
		"/workspace-invites/"+url.PathEscape(invite.InviteToken)+"/accept",
		beta.Token,
		nil,
		http.StatusOK,
	)

	workspaceConversations := mustJSON[api.CollectionResponse[api.Conversation]](
		t,
		h,
		http.MethodGet,
		"/conversations?workspace_id="+url.QueryEscape(workspace.ID),
		alpha.Token,
		nil,
		http.StatusOK,
	)
	if len(workspaceConversations.Items) == 0 {
		t.Fatalf("workspace %s returned no conversations", workspace.ID)
	}
	generalConversation := workspaceConversations.Items[0]

	privateTitle := "Private " + conversationQuery
	privateConversation := mustJSON[api.Conversation](
		t,
		h,
		http.MethodPost,
		"/conversations",
		alpha.Token,
		api.CreateConversationRequest{
			WorkspaceID:        &workspace.ID,
			AccessPolicy:       "members",
			ParticipantUserIDs: []string{beta.User.ID},
			Title:              &privateTitle,
		},
		http.StatusCreated,
	)

	globalConversation := mustJSON[api.Conversation](
		t,
		h,
		http.MethodPost,
		"/conversations",
		alpha.Token,
		api.CreateConversationRequest{
			WorkspaceID:        nil,
			ParticipantUserIDs: []string{gamma.User.ID},
		},
		http.StatusCreated,
	)

	workspaceMessage := mustJSON[api.Message](
		t,
		h,
		http.MethodPost,
		"/conversations/"+generalConversation.ID+"/messages",
		alpha.Token,
		api.CreateMessageRequest{BodyText: "workspace signal " + workspaceMessageQuery},
		http.StatusCreated,
	)
	privateMessage := mustJSON[api.Message](
		t,
		h,
		http.MethodPost,
		"/conversations/"+privateConversation.ID+"/messages",
		alpha.Token,
		api.CreateMessageRequest{BodyText: "private signal " + privateMessageQuery},
		http.StatusCreated,
	)
	globalMessage := mustJSON[api.Message](
		t,
		h,
		http.MethodPost,
		"/conversations/"+globalConversation.ID+"/messages",
		alpha.Token,
		api.CreateMessageRequest{BodyText: "global signal " + globalMessageQuery},
		http.StatusCreated,
	)

	workspaceHit := h.waitForSearchHit(t, alpha.Token, api.SearchRequest{
		Query: workspaceQuery,
		Kinds: []string{"workspace"},
	}, func(hit api.SearchHit) bool {
		return hit.Kind == "workspace" && hit.ResourceID == workspace.ID
	})
	if workspaceHit.Workspace == nil || workspaceHit.Workspace.ID != workspace.ID {
		t.Fatalf("workspace search hit = %+v, want workspace %s", workspaceHit, workspace.ID)
	}

	conversationHit := h.waitForSearchHit(t, alpha.Token, api.SearchRequest{
		Query: conversationQuery,
		Kinds: []string{"conversation"},
	}, func(hit api.SearchHit) bool {
		return hit.Kind == "conversation" && hit.ResourceID == privateConversation.ID
	})
	if conversationHit.Conversation == nil || conversationHit.Conversation.ID != privateConversation.ID {
		t.Fatalf("conversation search hit = %+v, want conversation %s", conversationHit, privateConversation.ID)
	}

	userHit := h.waitForSearchHit(t, alpha.Token, api.SearchRequest{
		Query:       userQuery,
		Kinds:       []string{"user"},
		WorkspaceID: &workspace.ID,
	}, func(hit api.SearchHit) bool {
		return hit.Kind == "user" && hit.ResourceID == updatedBeta.ID
	})
	if userHit.User == nil || userHit.User.ID != updatedBeta.ID {
		t.Fatalf("user search hit = %+v, want user %s", userHit, updatedBeta.ID)
	}

	workspaceMessageHit := h.waitForSearchHit(t, beta.Token, api.SearchRequest{
		Query:       workspaceMessageQuery,
		Kinds:       []string{"message"},
		WorkspaceID: &workspace.ID,
	}, func(hit api.SearchHit) bool {
		return hit.Kind == "message" && hit.ResourceID == workspaceMessage.ID
	})
	if workspaceMessageHit.Message == nil || workspaceMessageHit.Message.ID != workspaceMessage.ID {
		t.Fatalf("workspace message search hit = %+v, want message %s", workspaceMessageHit, workspaceMessage.ID)
	}

	privateMessageHit := h.waitForSearchHit(t, beta.Token, api.SearchRequest{
		Query:          privateMessageQuery,
		Kinds:          []string{"message"},
		ConversationID: &privateConversation.ID,
	}, func(hit api.SearchHit) bool {
		return hit.Kind == "message" && hit.ResourceID == privateMessage.ID
	})
	if privateMessageHit.Message == nil || privateMessageHit.Message.ID != privateMessage.ID {
		t.Fatalf("private message search hit = %+v, want message %s", privateMessageHit, privateMessage.ID)
	}

	globalMessageHit := h.waitForSearchHit(t, gamma.Token, api.SearchRequest{
		Query: globalMessageQuery,
		Kinds: []string{"message"},
	}, func(hit api.SearchHit) bool {
		return hit.Kind == "message" && hit.ResourceID == globalMessage.ID
	})
	if globalMessageHit.Message == nil || globalMessageHit.Message.ID != globalMessage.ID {
		t.Fatalf("global message search hit = %+v, want message %s", globalMessageHit, globalMessage.ID)
	}

	forbiddenWorkspace := h.search(t, gamma.Token, api.SearchRequest{
		Query: workspaceMessageQuery,
		Kinds: []string{"message"},
	})
	for _, item := range forbiddenWorkspace.Items {
		if item.ResourceID == workspaceMessage.ID {
			t.Fatalf("workspace message %s leaked to non-member search response", workspaceMessage.ID)
		}
	}

	forbiddenPrivate := h.search(t, gamma.Token, api.SearchRequest{
		Query: privateMessageQuery,
		Kinds: []string{"message"},
	})
	for _, item := range forbiddenPrivate.Items {
		if item.ResourceID == privateMessage.ID {
			t.Fatalf("private message %s leaked to non-participant search response", privateMessage.ID)
		}
	}

	forbiddenWorkspaceDoc := h.search(t, gamma.Token, api.SearchRequest{
		Query: workspaceQuery,
		Kinds: []string{"workspace"},
	})
	for _, item := range forbiddenWorkspaceDoc.Items {
		if item.ResourceID == workspace.ID {
			t.Fatalf("workspace %s leaked to non-member search response", workspace.ID)
		}
	}
}

func TestSPECWorkflows_WorkspaceInviteIdempotenceAndPrivateConversation(t *testing.T) {
	h := newWorkflowHarness(t)
	alpha := h.loginUser(t, "alpha@example.com")
	beta := h.loginUser(t, "beta@example.com")

	workspace := mustJSON[api.Workspace](
		t,
		h,
		http.MethodPost,
		"/workspaces",
		alpha.Token,
		api.CreateWorkspaceRequest{Name: "Acme", Slug: h.uniqueSlug("acme")},
		http.StatusCreated,
	)
	invite := mustJSON[api.CreateWorkspaceInviteResponse](
		t,
		h,
		http.MethodPost,
		"/workspaces/"+workspace.ID+"/invites",
		alpha.Token,
		api.CreateWorkspaceInviteRequest{Email: &beta.Email},
		http.StatusCreated,
	)

	member := mustJSON[api.WorkspaceMember](
		t,
		h,
		http.MethodPost,
		"/workspace-invites/"+url.PathEscape(invite.InviteToken)+"/accept",
		beta.Token,
		nil,
		http.StatusOK,
	)
	if member.Status != "active" || member.WorkspaceID != workspace.ID {
		t.Fatalf("first invite accept returned %+v", member)
	}

	member = mustJSON[api.WorkspaceMember](
		t,
		h,
		http.MethodPost,
		"/workspace-invites/"+url.PathEscape(invite.InviteToken)+"/accept",
		beta.Token,
		nil,
		http.StatusOK,
	)
	if member.Status != "active" || member.WorkspaceID != workspace.ID {
		t.Fatalf("second invite accept returned %+v", member)
	}

	if count := h.mustCountInternalEvents(t, "workspace.membership.added", workspace.ID, beta.User.ID); count != 1 {
		t.Fatalf("workspace.membership.added count for beta = %d, want 1", count)
	}

	privateConversation := mustJSON[api.Conversation](
		t,
		h,
		http.MethodPost,
		"/conversations",
		alpha.Token,
		api.CreateConversationRequest{
			WorkspaceID:        &workspace.ID,
			AccessPolicy:       "members",
			ParticipantUserIDs: []string{beta.User.ID},
		},
		http.StatusCreated,
	)
	if privateConversation.WorkspaceID == nil || *privateConversation.WorkspaceID != workspace.ID {
		t.Fatalf("workspace private conversation returned workspace_id %v, want %s", privateConversation.WorkspaceID, workspace.ID)
	}

	mustJSON[api.Message](
		t,
		h,
		http.MethodPost,
		"/conversations/"+privateConversation.ID+"/messages",
		alpha.Token,
		api.CreateMessageRequest{BodyText: "private workspace hello"},
		http.StatusCreated,
	)

	workspaceConversations := mustJSON[api.CollectionResponse[api.Conversation]](
		t,
		h,
		http.MethodGet,
		"/conversations?workspace_id="+url.QueryEscape(workspace.ID),
		beta.Token,
		nil,
		http.StatusOK,
	)
	foundGeneral := false
	foundPrivate := false
	for _, conversation := range workspaceConversations.Items {
		if conversation.Title != nil && *conversation.Title == "general" {
			foundGeneral = true
		}
		if conversation.ID == privateConversation.ID {
			foundPrivate = true
		}
	}
	if !foundGeneral || !foundPrivate {
		t.Fatalf("workspace conversations visibility mismatch: general=%v private=%v items=%+v", foundGeneral, foundPrivate, workspaceConversations.Items)
	}

	messages := mustJSON[api.CollectionResponse[api.Message]](
		t,
		h,
		http.MethodGet,
		"/conversations/"+privateConversation.ID+"/messages",
		beta.Token,
		nil,
		http.StatusOK,
	)
	if len(messages.Items) != 1 || messages.Items[0].ConversationID != privateConversation.ID {
		t.Fatalf("workspace private message list returned %+v", messages.Items)
	}
}

func TestSPECWorkflows_ConversationInviteAndWebhookSubscriptionDelivery(t *testing.T) {
	h := newWorkflowHarness(t)
	alpha := h.loginUser(t, "alpha@example.com")
	beta := h.loginUser(t, "beta@example.com")

	privateConversation := mustJSON[api.Conversation](
		t,
		h,
		http.MethodPost,
		"/conversations",
		alpha.Token,
		api.CreateConversationRequest{
			WorkspaceID:  nil,
			AccessPolicy: "members",
		},
		http.StatusCreated,
	)
	if privateConversation.ParticipantCount != 1 {
		t.Fatalf("single-owner private conversation participant_count = %d, want 1", privateConversation.ParticipantCount)
	}

	if privateConversation.ShareLink == nil {
		t.Fatalf("private conversation share_link = nil, want share link")
	}
	acceptedConversation := mustJSON[api.Conversation](
		t,
		h,
		http.MethodPost,
		"/conversations/join",
		beta.Token,
		api.JoinConversationRequest{Token: privateConversation.ShareLink.Token},
		http.StatusOK,
	)
	if acceptedConversation.ID != privateConversation.ID {
		t.Fatalf("accepted invite returned conversation %s, want %s", acceptedConversation.ID, privateConversation.ID)
	}

	participants := mustJSON[api.CollectionResponse[api.User]](
		t,
		h,
		http.MethodGet,
		"/conversations/"+privateConversation.ID+"/participants",
		beta.Token,
		nil,
		http.StatusOK,
	)
	participantIDs := make([]string, 0, len(participants.Items))
	for _, participant := range participants.Items {
		participantIDs = append(participantIDs, participant.ID)
	}
	if !slices.Contains(participantIDs, alpha.User.ID) || !slices.Contains(participantIDs, beta.User.ID) {
		t.Fatalf("conversation participants = %v, want %s and %s", participantIDs, alpha.User.ID, beta.User.ID)
	}

	joinEvent := h.waitForExternalEventMatch(
		t,
		alpha.Token,
		"/events?type=conversation.participant.added&resource_type=conversation&resource_id="+url.QueryEscape(privateConversation.ID),
		func(event api.ExternalEvent) bool {
			return event.Payload["user_id"] == beta.User.ID
		},
	)
	if joinEvent.ResourceType != "conversation" || joinEvent.ResourceID != privateConversation.ID {
		t.Fatalf("join event resource = %s/%s, want conversation/%s", joinEvent.ResourceType, joinEvent.ResourceID, privateConversation.ID)
	}
	if joinEvent.Payload["conversation_id"] != privateConversation.ID {
		t.Fatalf("join event payload conversation_id = %v, want %s", joinEvent.Payload["conversation_id"], privateConversation.ID)
	}
	if joinEvent.Payload["user_id"] != beta.User.ID {
		t.Fatalf("join event payload user_id = %v, want %s", joinEvent.Payload["user_id"], beta.User.ID)
	}

	recorder := newWebhookRecorder(t)
	secret := "shared-secret"
	subscription := mustJSON[api.EventSubscription](
		t,
		h,
		http.MethodPost,
		"/event-subscriptions",
		alpha.Token,
		api.CreateEventSubscriptionRequest{
			URL:          recorder.ContainerURL(),
			EventType:    stringPtr("conversation.message.created"),
			ResourceType: stringPtr("conversation"),
			ResourceID:   &privateConversation.ID,
			Secret:       secret,
		},
		http.StatusCreated,
	)

	encryptedSecret := h.mustEncryptedSubscriptionSecret(t, subscription.ID)
	if encryptedSecret == "" || encryptedSecret == secret {
		t.Fatalf("encrypted secret = %q, expected encrypted value", encryptedSecret)
	}
	decryptedSecret, err := h.protector.DecryptString(context.Background(), encryptedSecret)
	if err != nil {
		t.Fatalf("decrypt stored subscription secret: %v", err)
	}
	if decryptedSecret != secret {
		t.Fatalf("decrypted secret = %q, want %q", decryptedSecret, secret)
	}

	message := mustJSON[api.Message](
		t,
		h,
		http.MethodPost,
		"/conversations/"+privateConversation.ID+"/messages",
		alpha.Token,
		api.CreateMessageRequest{BodyText: "hello webhook"},
		http.StatusCreated,
	)

	record := recorder.waitForRecord(t)
	if record.Signature == "" {
		t.Fatal("webhook delivery omitted X-Teraslack-Signature")
	}
	expectedSignature := teracrypto.HMACSHA256Hex(secret, string(record.Body))
	if record.Signature != expectedSignature {
		t.Fatalf("webhook signature = %s, want %s", record.Signature, expectedSignature)
	}

	var event api.ExternalEvent
	if err := json.Unmarshal(record.Body, &event); err != nil {
		t.Fatalf("decode delivered webhook body: %v", err)
	}
	if event.Type != "conversation.message.created" {
		t.Fatalf("webhook event type = %s, want conversation.message.created", event.Type)
	}
	if event.ResourceType != "conversation" || event.ResourceID != privateConversation.ID {
		t.Fatalf("webhook event resource = %s/%s, want conversation/%s", event.ResourceType, event.ResourceID, privateConversation.ID)
	}
	if event.Payload["message_id"] != message.ID {
		t.Fatalf("webhook payload message_id = %v, want %s", event.Payload["message_id"], message.ID)
	}
}

func TestSPECWorkflows_GlobalConversationParticipantCanInviteAdditionalMembers(t *testing.T) {
	h := newWorkflowHarness(t)
	alpha := h.loginUser(t, "alpha@example.com")
	beta := h.loginUser(t, "beta@example.com")
	gamma := h.loginUser(t, "gamma@example.com")
	delta := h.loginUser(t, "delta@example.com")

	conversation := mustJSON[api.Conversation](
		t,
		h,
		http.MethodPost,
		"/conversations",
		alpha.Token,
		api.CreateConversationRequest{
			WorkspaceID:        nil,
			AccessPolicy:       "members",
			ParticipantUserIDs: []string{beta.User.ID, gamma.User.ID},
		},
		http.StatusCreated,
	)

	participants := mustJSON[api.CollectionResponse[api.User]](
		t,
		h,
		http.MethodPost,
		"/conversations/"+conversation.ID+"/participants",
		beta.Token,
		api.AddParticipantsRequest{
			UserIDs: []string{delta.User.ID},
		},
		http.StatusOK,
	)
	participantIDs := make([]string, 0, len(participants.Items))
	for _, participant := range participants.Items {
		participantIDs = append(participantIDs, participant.ID)
	}
	if !slices.Contains(participantIDs, delta.User.ID) {
		t.Fatalf("conversation participants = %v, want to include %s", participantIDs, delta.User.ID)
	}

	errResponse := mustJSON[api.ErrorResponse](
		t,
		h,
		http.MethodPatch,
		"/conversations/"+conversation.ID,
		beta.Token,
		api.UpdateConversationRequest{
			Title: stringPtr("renamed-by-participant"),
		},
		http.StatusForbidden,
	)
	if errResponse.Code != "forbidden" {
		t.Fatalf("participant metadata update error code = %s, want forbidden", errResponse.Code)
	}
}

func TestSPECWorkflows_WorkspaceConversationParticipantCanInviteAdditionalMembers(t *testing.T) {
	h := newWorkflowHarness(t)
	alpha := h.loginUser(t, "alpha@example.com")
	beta := h.loginUser(t, "beta@example.com")
	gamma := h.loginUser(t, "gamma@example.com")

	workspace := mustJSON[api.Workspace](
		t,
		h,
		http.MethodPost,
		"/workspaces",
		alpha.Token,
		api.CreateWorkspaceRequest{Name: "Acme", Slug: h.uniqueSlug("acme")},
		http.StatusCreated,
	)

	for _, actor := range []actor{beta, gamma} {
		invite := mustJSON[api.CreateWorkspaceInviteResponse](
			t,
			h,
			http.MethodPost,
			"/workspaces/"+workspace.ID+"/invites",
			alpha.Token,
			api.CreateWorkspaceInviteRequest{Email: stringPtr(actor.Email)},
			http.StatusCreated,
		)
		mustJSON[api.WorkspaceMember](
			t,
			h,
			http.MethodPost,
			"/workspace-invites/"+url.PathEscape(invite.InviteToken)+"/accept",
			actor.Token,
			nil,
			http.StatusOK,
		)
	}

	conversation := mustJSON[api.Conversation](
		t,
		h,
		http.MethodPost,
		"/conversations",
		alpha.Token,
		api.CreateConversationRequest{
			WorkspaceID:        &workspace.ID,
			AccessPolicy:       "members",
			ParticipantUserIDs: []string{beta.User.ID},
		},
		http.StatusCreated,
	)

	participants := mustJSON[api.CollectionResponse[api.User]](
		t,
		h,
		http.MethodPost,
		"/conversations/"+conversation.ID+"/participants",
		beta.Token,
		api.AddParticipantsRequest{
			UserIDs: []string{gamma.User.ID},
		},
		http.StatusOK,
	)
	participantIDs := make([]string, 0, len(participants.Items))
	for _, participant := range participants.Items {
		participantIDs = append(participantIDs, participant.ID)
	}
	if !slices.Contains(participantIDs, gamma.User.ID) {
		t.Fatalf("workspace conversation participants = %v, want to include %s", participantIDs, gamma.User.ID)
	}
}

func TestSPECWorkflows_AgentCreationGeneratesDisplayName(t *testing.T) {
	h := newWorkflowHarness(t)
	alpha := h.loginUser(t, "alpha@example.com")

	created := mustJSON[api.CreateAgentResponse](
		t,
		h,
		http.MethodPost,
		"/agents",
		alpha.Token,
		api.CreateAgentRequest{
			OwnerType: "user",
		},
		http.StatusCreated,
	)
	agent := created.Agent
	if agent.User.PrincipalType != "agent" {
		t.Fatalf("created principal_type = %q, want agent", agent.User.PrincipalType)
	}
	if strings.TrimSpace(agent.User.Profile.DisplayName) == "" {
		t.Fatalf("created agent display_name is blank")
	}
	if strings.TrimSpace(agent.User.Profile.Handle) == "" {
		t.Fatalf("created agent handle is blank")
	}
	if agent.OwnerType != "user" {
		t.Fatalf("created agent owner_type = %q, want user", agent.OwnerType)
	}
	if strings.TrimSpace(created.APIKey.Token) == "" {
		t.Fatal("created agent api_key.token is blank")
	}
	if created.APIKey.ScopeType != "user" {
		t.Fatalf("created agent api_key.scope_type = %q, want user", created.APIKey.ScopeType)
	}
	if created.APIKey.ScopeWorkspaceID != nil {
		t.Fatalf("created agent api_key.scope_workspace_id = %v, want nil", *created.APIKey.ScopeWorkspaceID)
	}

	currentKey := mustJSON[api.AgentAPIKey](
		t,
		h,
		http.MethodGet,
		"/agents/"+agent.User.ID+"/api-key",
		alpha.Token,
		nil,
		http.StatusOK,
	)
	if currentKey.Token != created.APIKey.Token {
		t.Fatalf("get agent api key token = %q, want created token %q", currentKey.Token, created.APIKey.Token)
	}

	me := mustJSON[api.MeResponse](t, h, http.MethodGet, "/me", created.APIKey.Token, nil, http.StatusOK)
	if me.User.ID != agent.User.ID {
		t.Fatalf("agent key authenticated as user %s, want %s", me.User.ID, agent.User.ID)
	}
	if me.User.PrincipalType != "agent" {
		t.Fatalf("agent key principal_type = %q, want agent", me.User.PrincipalType)
	}

	errResponse := mustJSON[api.ErrorResponse](
		t,
		h,
		http.MethodPost,
		"/agents",
		alpha.Token,
		api.CreateAgentRequest{
			DisplayName: stringPtr("Custom Bot"),
			OwnerType:   "user",
		},
		http.StatusUnprocessableEntity,
	)
	if errResponse.Code != "validation_failed" {
		t.Fatalf("explicit display_name error code = %s, want validation_failed", errResponse.Code)
	}
}

func TestSPECWorkflows_AgentMetadataRoundTripsThroughAPI(t *testing.T) {
	h := newWorkflowHarness(t)
	alpha := h.loginUser(t, "alpha@example.com")

	createMetadata := map[string]any{
		"role":       "session-agent",
		"session_id": "session-123",
	}
	created := mustJSON[api.CreateAgentResponse](
		t,
		h,
		http.MethodPost,
		"/agents",
		alpha.Token,
		api.CreateAgentRequest{
			OwnerType: "user",
			Metadata:  &createMetadata,
		},
		http.StatusCreated,
	)
	if created.Agent.Metadata["role"] != "session-agent" {
		t.Fatalf("created agent metadata role = %#v", created.Agent.Metadata["role"])
	}
	if created.Agent.Metadata["session_id"] != "session-123" {
		t.Fatalf("created agent metadata session_id = %#v", created.Agent.Metadata["session_id"])
	}

	listed := mustJSON[api.CollectionResponse[api.Agent]](
		t,
		h,
		http.MethodGet,
		"/agents",
		alpha.Token,
		nil,
		http.StatusOK,
	)
	var listedAgent *api.Agent
	for idx := range listed.Items {
		if listed.Items[idx].User.ID == created.Agent.User.ID {
			listedAgent = &listed.Items[idx]
			break
		}
	}
	if listedAgent == nil {
		t.Fatalf("list agents did not include %s", created.Agent.User.ID)
	}
	if listedAgent.Metadata["session_id"] != "session-123" {
		t.Fatalf("listed agent metadata session_id = %#v", listedAgent.Metadata["session_id"])
	}

	updateMetadata := map[string]any{
		"role":       "session-agent",
		"session_id": "session-456",
		"shell":      "zsh",
	}
	updated := mustJSON[api.Agent](
		t,
		h,
		http.MethodPatch,
		"/agents/"+created.Agent.User.ID,
		alpha.Token,
		api.UpdateAgentRequest{
			Metadata: &updateMetadata,
		},
		http.StatusOK,
	)
	if updated.Metadata["session_id"] != "session-456" {
		t.Fatalf("updated agent metadata session_id = %#v", updated.Metadata["session_id"])
	}
	if updated.Metadata["shell"] != "zsh" {
		t.Fatalf("updated agent metadata shell = %#v", updated.Metadata["shell"])
	}

	fetched := mustJSON[api.Agent](
		t,
		h,
		http.MethodGet,
		"/agents/"+created.Agent.User.ID,
		alpha.Token,
		nil,
		http.StatusOK,
	)
	if fetched.Metadata["session_id"] != "session-456" {
		t.Fatalf("fetched agent metadata session_id = %#v", fetched.Metadata["session_id"])
	}
	if fetched.Metadata["shell"] != "zsh" {
		t.Fatalf("fetched agent metadata shell = %#v", fetched.Metadata["shell"])
	}
}

func TestSPECWorkflows_AgentAPIKeyRotationRevokesPreviousToken(t *testing.T) {
	h := newWorkflowHarness(t)
	alpha := h.loginUser(t, "alpha@example.com")

	created := mustJSON[api.CreateAgentResponse](
		t,
		h,
		http.MethodPost,
		"/agents",
		alpha.Token,
		api.CreateAgentRequest{
			OwnerType: "user",
		},
		http.StatusCreated,
	)

	rotated := mustJSON[api.AgentAPIKey](
		t,
		h,
		http.MethodPost,
		"/agents/"+created.Agent.User.ID+"/api-key/rotate",
		alpha.Token,
		nil,
		http.StatusOK,
	)
	if rotated.ID == created.APIKey.ID {
		t.Fatalf("rotated agent api key id = %q, want a new id", rotated.ID)
	}
	if rotated.Token == created.APIKey.Token {
		t.Fatalf("rotated agent api key token = %q, want a new token", rotated.Token)
	}

	errResponse := mustJSON[api.ErrorResponse](t, h, http.MethodGet, "/me", created.APIKey.Token, nil, http.StatusUnauthorized)
	if errResponse.Code != "unauthorized" {
		t.Fatalf("old agent api key error code = %s, want unauthorized", errResponse.Code)
	}

	me := mustJSON[api.MeResponse](t, h, http.MethodGet, "/me", rotated.Token, nil, http.StatusOK)
	if me.User.ID != created.Agent.User.ID {
		t.Fatalf("rotated agent api key authenticated as user %s, want %s", me.User.ID, created.Agent.User.ID)
	}
}

func TestSPECWorkflows_EventSubscriptionValidation(t *testing.T) {
	h := newWorkflowHarness(t)
	alpha := h.loginUser(t, "alpha@example.com")

	conversation := mustJSON[api.Conversation](
		t,
		h,
		http.MethodPost,
		"/conversations",
		alpha.Token,
		api.CreateConversationRequest{
			WorkspaceID:  nil,
			AccessPolicy: "members",
		},
		http.StatusCreated,
	)

	errResponse := mustJSON[api.ErrorResponse](
		t,
		h,
		http.MethodPost,
		"/event-subscriptions",
		alpha.Token,
		api.CreateEventSubscriptionRequest{
			URL:          "https://hooks.example.com/teraslack",
			ResourceType: stringPtr("message"),
			ResourceID:   &conversation.ID,
			Secret:       "shared-secret",
		},
		http.StatusUnprocessableEntity,
	)
	if errResponse.Code != "validation_failed" {
		t.Fatalf("unsupported resource type error code = %s, want validation_failed", errResponse.Code)
	}

	errResponse = mustJSON[api.ErrorResponse](
		t,
		h,
		http.MethodPost,
		"/event-subscriptions",
		alpha.Token,
		api.CreateEventSubscriptionRequest{
			URL:          "https://hooks.example.com/teraslack",
			ResourceType: stringPtr("conversation"),
			ResourceID:   &conversation.ID,
			Secret:       "   ",
		},
		http.StatusUnprocessableEntity,
	)
	if errResponse.Code != "validation_failed" {
		t.Fatalf("blank secret error code = %s, want validation_failed", errResponse.Code)
	}

	subscription := mustJSON[api.EventSubscription](
		t,
		h,
		http.MethodPost,
		"/event-subscriptions",
		alpha.Token,
		api.CreateEventSubscriptionRequest{
			URL:    "https://hooks.example.com/teraslack",
			Secret: "shared-secret",
		},
		http.StatusCreated,
	)

	errResponse = mustJSON[api.ErrorResponse](
		t,
		h,
		http.MethodPatch,
		"/event-subscriptions/"+subscription.ID,
		alpha.Token,
		nil,
		http.StatusUnprocessableEntity,
	)
	if errResponse.Code != "validation_failed" {
		t.Fatalf("missing enabled error code = %s, want validation_failed", errResponse.Code)
	}

	errResponse = mustJSON[api.ErrorResponse](
		t,
		h,
		http.MethodGet,
		"/events?resource_type=message",
		alpha.Token,
		nil,
		http.StatusUnprocessableEntity,
	)
	if errResponse.Code != "validation_failed" {
		t.Fatalf("unsupported events filter error code = %s, want validation_failed", errResponse.Code)
	}
}

func TestSPECWorkflows_ProjectorSkipsMalformedInternalEvents(t *testing.T) {
	h := newWorkflowHarness(t)
	alpha := h.loginUser(t, "alpha@example.com")

	userID, err := uuid.Parse(alpha.User.ID)
	if err != nil {
		t.Fatalf("parse user id %s: %v", alpha.User.ID, err)
	}
	shardID := eventsourcing.ShardForAggregate(userID)
	now := time.Now().UTC()

	tx, err := h.pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(context.Background())

	invalidEventID, err := eventsourcing.AppendInternalEvent(context.Background(), tx, eventsourcing.InternalEvent{
		EventType:     "workspace.updated",
		AggregateType: "workspace",
		AggregateID:   userID,
		ActorUserID:   &userID,
		ShardID:       shardID,
		Payload: map[string]any{
			"name": "missing-workspace-id",
		},
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("append malformed internal event: %v", err)
	}
	validEventID, err := eventsourcing.AppendInternalEvent(context.Background(), tx, eventsourcing.InternalEvent{
		EventType:     "user.updated",
		AggregateType: "user",
		AggregateID:   userID,
		ActorUserID:   &userID,
		ShardID:       shardID,
		Payload: map[string]any{
			"user_id": userID.String(),
		},
		CreatedAt: now.Add(time.Millisecond),
	})
	if err != nil {
		t.Fatalf("append valid internal event: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}

	h.waitForProjectionFailureCount(t, invalidEventID, 1)
	h.waitForProjectedExternalEventCount(t, validEventID, 1)
}

func TestSPECWorkflows_WebhookWorkerClaimsDeliveriesOnce(t *testing.T) {
	h := newWorkflowHarness(t)
	alpha := h.loginUser(t, "alpha@example.com")
	h.mustScaleService(t, "webhook-worker", 2)
	t.Cleanup(func() {
		h.mustScaleService(t, "webhook-worker", 1)
	})

	recorder := newWebhookRecorderWithDelay(t, 250*time.Millisecond)
	conversation := mustJSON[api.Conversation](
		t,
		h,
		http.MethodPost,
		"/conversations",
		alpha.Token,
		api.CreateConversationRequest{
			WorkspaceID:  nil,
			AccessPolicy: "members",
		},
		http.StatusCreated,
	)

	mustJSON[api.EventSubscription](
		t,
		h,
		http.MethodPost,
		"/event-subscriptions",
		alpha.Token,
		api.CreateEventSubscriptionRequest{
			URL:          recorder.ContainerURL(),
			EventType:    stringPtr("conversation.message.created"),
			ResourceType: stringPtr("conversation"),
			ResourceID:   &conversation.ID,
			Secret:       "shared-secret",
		},
		http.StatusCreated,
	)
	mustJSON[api.Message](
		t,
		h,
		http.MethodPost,
		"/conversations/"+conversation.ID+"/messages",
		alpha.Token,
		api.CreateMessageRequest{BodyText: "deliver once"},
		http.StatusCreated,
	)

	recorder.waitForRecord(t)
	time.Sleep(5 * time.Second)

	if got := recorder.recordCount(); got != 1 {
		t.Fatalf("webhook recorder saw %d deliveries, want 1", got)
	}
}

func stringPtr(value string) *string {
	return &value
}
