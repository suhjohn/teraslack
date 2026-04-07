package search

import "github.com/google/uuid"

var principalNamespace = uuid.MustParse("7cd14411-3578-4af8-8c5c-a77fb3300cec")
var searchDocumentNamespace = uuid.MustParse("bf1f5b31-1d54-4b59-bf8a-c40e582a0af4")

func userPrincipalID(userID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(principalNamespace, []byte("principal:user:"+userID.String()))
}

func workspacePrincipalID(workspaceID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(principalNamespace, []byte("principal:workspace:"+workspaceID.String()))
}

func conversationPrincipalID(conversationID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(principalNamespace, []byte("principal:conversation:"+conversationID.String()))
}
