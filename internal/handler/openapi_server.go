package handler

import (
	"net/http"

	openapi "github.com/suhjohn/teraslack/internal/api"
	"github.com/suhjohn/teraslack/pkg/httputil"
)

type openAPIServer struct {
	workspaceH        *WorkspaceHandler
	userH             *UserHandler
	convH             *ConversationHandler
	msgH              *MessageHandler
	ugH               *UsergroupHandler
	pinH              *PinHandler
	bookmarkH         *BookmarkHandler
	fileH             *FileHandler
	externalEventH    *ExternalEventHandler
	externalAccessH   *ExternalPrincipalAccessHandler
	eventH            *EventHandler
	authH             *AuthHandler
	searchH           *SearchHandler
	apiKeyH           *APIKeyHandler
	conversationReadH *ConversationReadHandler
}

var _ openapi.ServerInterface = (*openAPIServer)(nil)

func newOpenAPIServer(
	workspaceH *WorkspaceHandler,
	userH *UserHandler,
	convH *ConversationHandler,
	msgH *MessageHandler,
	ugH *UsergroupHandler,
	pinH *PinHandler,
	bookmarkH *BookmarkHandler,
	fileH *FileHandler,
	externalEventH *ExternalEventHandler,
	externalAccessH *ExternalPrincipalAccessHandler,
	eventH *EventHandler,
	authH *AuthHandler,
	searchH *SearchHandler,
	apiKeyH *APIKeyHandler,
	conversationReadH *ConversationReadHandler,
) *openAPIServer {
	return &openAPIServer{
		workspaceH:        workspaceH,
		userH:             userH,
		convH:             convH,
		msgH:              msgH,
		ugH:               ugH,
		pinH:              pinH,
		bookmarkH:         bookmarkH,
		fileH:             fileH,
		externalEventH:    externalEventH,
		externalAccessH:   externalAccessH,
		eventH:            eventH,
		authH:             authH,
		searchH:           searchH,
		apiKeyH:           apiKeyH,
		conversationReadH: conversationReadH,
	}
}

func (s *openAPIServer) ListApiKeys(w http.ResponseWriter, r *http.Request, _ openapi.ListApiKeysParams) {
	s.apiKeyH.List(w, r)
}

func (s *openAPIServer) ListExternalPrincipalAccess(w http.ResponseWriter, r *http.Request, _ openapi.ListExternalPrincipalAccessParams) {
	s.externalAccessH.List(w, r)
}

func (s *openAPIServer) CreateExternalPrincipalAccess(w http.ResponseWriter, r *http.Request) {
	s.externalAccessH.Create(w, r)
}

func (s *openAPIServer) GetExternalPrincipalAccess(w http.ResponseWriter, r *http.Request, _ openapi.ExternalPrincipalAccessIDPath) {
	s.externalAccessH.Get(w, r)
}

func (s *openAPIServer) UpdateExternalPrincipalAccess(w http.ResponseWriter, r *http.Request, _ openapi.ExternalPrincipalAccessIDPath) {
	s.externalAccessH.Update(w, r)
}

func (s *openAPIServer) DeleteExternalPrincipalAccess(w http.ResponseWriter, r *http.Request, _ openapi.ExternalPrincipalAccessIDPath) {
	s.externalAccessH.Delete(w, r)
}

func (s *openAPIServer) CreateApiKey(w http.ResponseWriter, r *http.Request) {
	s.apiKeyH.Create(w, r)
}

func (s *openAPIServer) DeleteApiKey(w http.ResponseWriter, r *http.Request, _ openapi.APIKeyIDPath) {
	s.apiKeyH.Delete(w, r)
}

func (s *openAPIServer) GetApiKey(w http.ResponseWriter, r *http.Request, _ openapi.APIKeyIDPath) {
	s.apiKeyH.Get(w, r)
}

func (s *openAPIServer) UpdateApiKey(w http.ResponseWriter, r *http.Request, _ openapi.APIKeyIDPath) {
	s.apiKeyH.Update(w, r)
}

func (s *openAPIServer) RotateApiKey(w http.ResponseWriter, r *http.Request, _ openapi.APIKeyIDPath) {
	s.apiKeyH.Rotate(w, r)
}

func (s *openAPIServer) GetAuthMe(w http.ResponseWriter, r *http.Request) {
	s.authH.Me(w, r)
}

func (s *openAPIServer) ListConversations(w http.ResponseWriter, r *http.Request, _ openapi.ListConversationsParams) {
	s.convH.List(w, r)
}

func (s *openAPIServer) CreateConversation(w http.ResponseWriter, r *http.Request) {
	s.convH.Create(w, r)
}

func (s *openAPIServer) DeleteBookmark(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPathNamed, _ openapi.BookmarkIDPath) {
	s.bookmarkH.Remove(w, r)
}

func (s *openAPIServer) UpdateBookmark(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPathNamed, _ openapi.BookmarkIDPath) {
	s.bookmarkH.Edit(w, r)
}

func (s *openAPIServer) GetConversation(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath) {
	s.convH.Info(w, r)
}

func (s *openAPIServer) UpdateConversation(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath) {
	s.convH.Update(w, r)
}

func (s *openAPIServer) ListBookmarks(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath) {
	s.bookmarkH.List(w, r)
}

func (s *openAPIServer) CreateBookmark(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath) {
	s.bookmarkH.Create(w, r)
}

func (s *openAPIServer) ListConversationMembers(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath, _ openapi.ListConversationMembersParams) {
	s.convH.Members(w, r)
}

func (s *openAPIServer) GetConversationManagers(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath) {
	s.convH.ListManagers(w, r)
}

func (s *openAPIServer) UpdateConversationManagers(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath) {
	s.convH.SetManagers(w, r)
}

func (s *openAPIServer) GetConversationPostingPolicy(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath) {
	s.convH.GetPostingPolicy(w, r)
}

func (s *openAPIServer) UpdateConversationPostingPolicy(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath) {
	s.convH.SetPostingPolicy(w, r)
}

func (s *openAPIServer) AddConversationMembers(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath) {
	s.convH.Invite(w, r)
}

func (s *openAPIServer) RemoveConversationMember(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath, _ openapi.UserIDPathNamed) {
	s.convH.Kick(w, r)
}

func (s *openAPIServer) ListPins(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath) {
	s.pinH.List(w, r)
}

func (s *openAPIServer) CreatePin(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath) {
	s.pinH.Add(w, r)
}

func (s *openAPIServer) DeletePin(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath, _ openapi.MessageTSPath) {
	s.pinH.Remove(w, r)
}

func (s *openAPIServer) UpdateConversationReadState(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPath) {
	s.conversationReadH.MarkRead(w, r)
}

func (s *openAPIServer) ListEventSubscriptions(w http.ResponseWriter, r *http.Request, _ openapi.ListEventSubscriptionsParams) {
	s.eventH.ListSubscriptions(w, r)
}

func (s *openAPIServer) CreateEventSubscription(w http.ResponseWriter, r *http.Request) {
	s.eventH.CreateSubscription(w, r)
}

func (s *openAPIServer) ListEvents(w http.ResponseWriter, r *http.Request, _ openapi.ListEventsParams) {
	s.externalEventH.List(w, r)
}

func (s *openAPIServer) DeleteEventSubscription(w http.ResponseWriter, r *http.Request, _ openapi.EventSubscriptionIDPath) {
	s.eventH.DeleteSubscription(w, r)
}

func (s *openAPIServer) GetEventSubscription(w http.ResponseWriter, r *http.Request, _ openapi.EventSubscriptionIDPath) {
	s.eventH.GetSubscription(w, r)
}

func (s *openAPIServer) UpdateEventSubscription(w http.ResponseWriter, r *http.Request, _ openapi.EventSubscriptionIDPath) {
	s.eventH.UpdateSubscription(w, r)
}

func (s *openAPIServer) CreateFileUpload(w http.ResponseWriter, r *http.Request) {
	s.fileH.GetUploadURL(w, r)
}

func (s *openAPIServer) CompleteFileUpload(w http.ResponseWriter, r *http.Request, _ openapi.FileIDPath) {
	s.fileH.CompleteUpload(w, r)
}

func (s *openAPIServer) ListFiles(w http.ResponseWriter, r *http.Request, _ openapi.ListFilesParams) {
	s.fileH.List(w, r)
}

func (s *openAPIServer) CreateRemoteFile(w http.ResponseWriter, r *http.Request) {
	s.fileH.AddRemoteFile(w, r)
}

func (s *openAPIServer) DeleteFile(w http.ResponseWriter, r *http.Request, _ openapi.FileIDPath) {
	s.fileH.Delete(w, r)
}

func (s *openAPIServer) GetFile(w http.ResponseWriter, r *http.Request, _ openapi.FileIDPath) {
	s.fileH.Info(w, r)
}

func (s *openAPIServer) CreateFileShare(w http.ResponseWriter, r *http.Request, _ openapi.FileIDPath) {
	s.fileH.ShareRemoteFile(w, r)
}

func (s *openAPIServer) GetHealth(w http.ResponseWriter, r *http.Request) {
	httputil.WriteResource(w, http.StatusOK, HealthStatusResponse{Status: "ok"})
}

func (s *openAPIServer) ListMessages(w http.ResponseWriter, r *http.Request, _ openapi.ListMessagesParams) {
	s.msgH.History(w, r)
}

func (s *openAPIServer) CreateMessage(w http.ResponseWriter, r *http.Request) {
	s.msgH.PostMessage(w, r)
}

func (s *openAPIServer) DeleteMessage(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPathNamed, _ openapi.MessageTSPath) {
	s.msgH.DeleteMessage(w, r)
}

func (s *openAPIServer) UpdateMessage(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPathNamed, _ openapi.MessageTSPath) {
	s.msgH.UpdateMessage(w, r)
}

func (s *openAPIServer) ListMessageReactions(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPathNamed, _ openapi.MessageTSPath) {
	s.msgH.GetReactions(w, r)
}

func (s *openAPIServer) CreateMessageReaction(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPathNamed, _ openapi.MessageTSPath) {
	s.msgH.AddReaction(w, r)
}

func (s *openAPIServer) DeleteMessageReaction(w http.ResponseWriter, r *http.Request, _ openapi.ConversationIDPathNamed, _ openapi.MessageTSPath, _ openapi.ReactionNamePath) {
	s.msgH.RemoveReaction(w, r)
}

func (s *openAPIServer) Search(w http.ResponseWriter, r *http.Request) {
	s.searchH.Search(w, r)
}

func (s *openAPIServer) ListTeams(w http.ResponseWriter, r *http.Request) {
	s.workspaceH.List(w, r)
}

func (s *openAPIServer) CreateTeam(w http.ResponseWriter, r *http.Request) {
	s.workspaceH.Create(w, r)
}

func (s *openAPIServer) GetTeam(w http.ResponseWriter, r *http.Request, _ openapi.TeamIDPath) {
	s.workspaceH.Get(w, r)
}

func (s *openAPIServer) UpdateTeam(w http.ResponseWriter, r *http.Request, _ openapi.TeamIDPath) {
	s.workspaceH.Update(w, r)
}

func (s *openAPIServer) ListTeamAccessLogs(w http.ResponseWriter, r *http.Request, _ openapi.TeamIDPath, _ openapi.ListTeamAccessLogsParams) {
	s.workspaceH.AccessLogs(w, r)
}

func (s *openAPIServer) ListTeamAuthorizationAuditLogs(w http.ResponseWriter, r *http.Request, _ openapi.TeamIDPath, _ openapi.ListTeamAuthorizationAuditLogsParams) {
	s.workspaceH.AuthorizationAuditLogs(w, r)
}

func (s *openAPIServer) ListTeamAdmins(w http.ResponseWriter, r *http.Request, _ openapi.TeamIDPath) {
	s.workspaceH.ListAdmins(w, r)
}

func (s *openAPIServer) GetTeamBillableInfo(w http.ResponseWriter, r *http.Request, _ openapi.TeamIDPath) {
	s.workspaceH.BillableInfo(w, r)
}

func (s *openAPIServer) GetTeamBilling(w http.ResponseWriter, r *http.Request, _ openapi.TeamIDPath) {
	s.workspaceH.Billing(w, r)
}

func (s *openAPIServer) ListExternalTeams(w http.ResponseWriter, r *http.Request, _ openapi.TeamIDPath) {
	s.workspaceH.ListExternalTeams(w, r)
}

func (s *openAPIServer) DisconnectExternalTeam(w http.ResponseWriter, r *http.Request, _ openapi.TeamIDPath, _ openapi.ExternalTeamIDPath) {
	s.workspaceH.DisconnectExternalTeam(w, r)
}

func (s *openAPIServer) ListTeamIntegrationLogs(w http.ResponseWriter, r *http.Request, _ openapi.TeamIDPath, _ openapi.ListTeamIntegrationLogsParams) {
	s.workspaceH.IntegrationLogs(w, r)
}

func (s *openAPIServer) TransferPrimaryAdmin(w http.ResponseWriter, r *http.Request, _ openapi.TeamIDPath) {
	s.workspaceH.TransferPrimaryAdmin(w, r)
}

func (s *openAPIServer) ListTeamOwners(w http.ResponseWriter, r *http.Request, _ openapi.TeamIDPath) {
	s.workspaceH.ListOwners(w, r)
}

func (s *openAPIServer) GetTeamPreferences(w http.ResponseWriter, r *http.Request, _ openapi.TeamIDPath) {
	s.workspaceH.Preferences(w, r)
}

func (s *openAPIServer) ListTeamProfileFields(w http.ResponseWriter, r *http.Request, _ openapi.TeamIDPath) {
	s.workspaceH.ProfileFields(w, r)
}

func (s *openAPIServer) CompleteOAuth(w http.ResponseWriter, r *http.Request, provider openapi.AuthProviderPath, _ openapi.CompleteOAuthParams) {
	r.SetPathValue("provider", string(provider))
	s.authH.CompleteOAuth(w, r)
}

func (s *openAPIServer) StartOAuth(w http.ResponseWriter, r *http.Request, provider openapi.AuthProviderPath, _ openapi.StartOAuthParams) {
	r.SetPathValue("provider", string(provider))
	s.authH.StartOAuth(w, r)
}

func (s *openAPIServer) DeleteCurrentSession(w http.ResponseWriter, r *http.Request) {
	s.authH.RevokeCurrentSession(w, r)
}

func (s *openAPIServer) ListUsergroups(w http.ResponseWriter, r *http.Request, _ openapi.ListUsergroupsParams) {
	s.ugH.List(w, r)
}

func (s *openAPIServer) CreateUsergroup(w http.ResponseWriter, r *http.Request) {
	s.ugH.Create(w, r)
}

func (s *openAPIServer) GetUsergroup(w http.ResponseWriter, r *http.Request, _ openapi.UsergroupIDPath) {
	s.ugH.Info(w, r)
}

func (s *openAPIServer) UpdateUsergroup(w http.ResponseWriter, r *http.Request, _ openapi.UsergroupIDPath) {
	s.ugH.Update(w, r)
}

func (s *openAPIServer) ListUsergroupMembers(w http.ResponseWriter, r *http.Request, _ openapi.UsergroupIDPath) {
	s.ugH.ListUsers(w, r)
}

func (s *openAPIServer) ReplaceUsergroupMembers(w http.ResponseWriter, r *http.Request, _ openapi.UsergroupIDPath) {
	s.ugH.SetUsers(w, r)
}

func (s *openAPIServer) ListUsers(w http.ResponseWriter, r *http.Request, _ openapi.ListUsersParams) {
	s.userH.List(w, r)
}

func (s *openAPIServer) CreateUser(w http.ResponseWriter, r *http.Request) {
	s.userH.Create(w, r)
}

func (s *openAPIServer) GetUser(w http.ResponseWriter, r *http.Request, _ openapi.UserIDPath) {
	s.userH.Info(w, r)
}

func (s *openAPIServer) UpdateUser(w http.ResponseWriter, r *http.Request, _ openapi.UserIDPath) {
	s.userH.Update(w, r)
}

func (s *openAPIServer) GetUserRoles(w http.ResponseWriter, r *http.Request, _ openapi.UserIDPath) {
	s.userH.ListRoles(w, r)
}

func (s *openAPIServer) UpdateUserRoles(w http.ResponseWriter, r *http.Request, _ openapi.UserIDPath) {
	s.userH.SetRoles(w, r)
}
