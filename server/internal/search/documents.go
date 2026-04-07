package search

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Runtime) buildDocumentsForResource(ctx context.Context, resourceKind string, resourceID string) ([]searchDocument, error) {
	switch resourceKind {
	case documentKindMessage:
		messageID, err := uuid.Parse(resourceID)
		if err != nil {
			return nil, err
		}
		return r.buildMessageDocuments(ctx, messageID)
	case documentKindConversation:
		conversationID, err := uuid.Parse(resourceID)
		if err != nil {
			return nil, err
		}
		return r.buildConversationDocuments(ctx, conversationID)
	case documentKindWorkspace:
		workspaceID, err := uuid.Parse(resourceID)
		if err != nil {
			return nil, err
		}
		return r.buildWorkspaceDocuments(ctx, workspaceID)
	case documentKindUser:
		userID, err := uuid.Parse(resourceID)
		if err != nil {
			return nil, err
		}
		return r.buildUserDocuments(ctx, userID)
	case documentKindEvent:
		eventID, err := parseEventID(resourceID)
		if err != nil {
			return nil, err
		}
		return r.buildEventDocuments(ctx, eventID)
	default:
		return nil, fmt.Errorf("unsupported resource kind %q", resourceKind)
	}
}

func parseEventID(value string) (int64, error) {
	var eventID int64
	_, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &eventID)
	if err != nil || eventID <= 0 {
		return 0, fmt.Errorf("invalid event id %q", value)
	}
	return eventID, nil
}

func (r *Runtime) buildMessageDocuments(ctx context.Context, messageID uuid.UUID) ([]searchDocument, error) {
	message, err := r.loadMessage(ctx, messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if message.DeletedAt != nil {
		return nil, nil
	}

	conversation, err := r.loadConversation(ctx, message.ConversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var workspaceName string
	if conversation.WorkspaceID != nil {
		workspace, err := r.loadWorkspace(ctx, *conversation.WorkspaceID)
		if err == nil {
			workspaceName = workspace.Name
		}
	}
	author, _ := r.loadUser(ctx, message.AuthorUserID)
	title := r.conversationTitle(ctx, conversation)
	bodyText := messageText(message)
	body := strings.Join(nonEmpty(bodyText, workspaceName, author.DisplayName, "@"+author.Handle), "\n")
	documents := make([]searchDocument, 0)
	for _, anchor := range conversationAccessAnchors(conversation) {
		documents = append(documents, searchDocument{
			Kind:             documentKindMessage,
			CanonicalID:      message.ID.String(),
			ResultKey:        resultKey(documentKindMessage, message.ID.String()),
			DocID:            documentID(documentKindMessage, message.ID.String(), anchor),
			WorkspaceID:      anchor.WorkspaceID,
			ConversationID:   &conversation.ID,
			ReadPrincipalIDs: []uuid.UUID{anchor.PrincipalID},
			Title:            stringValue(title),
			Body:             body,
			ExactTerms:       namedDocumentTerms(stringValue(title), workspaceName, author.DisplayName, "@"+author.Handle),
			CreatedAt:        message.CreatedAt.UTC(),
			UpdatedAt:        currentUpdatedAt(message.CreatedAt, message.EditedAt),
			Archived:         conversation.ArchivedAt != nil,
			EmbeddingText:    strings.Join(nonEmpty(stringValue(title), bodyText, workspaceName, author.DisplayName, author.Handle), "\n"),
		})
	}
	return mergeDocumentsByID(documents), nil
}

func (r *Runtime) buildConversationDocuments(ctx context.Context, conversationID uuid.UUID) ([]searchDocument, error) {
	conversation, err := r.loadConversation(ctx, conversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	title := r.conversationTitle(ctx, conversation)
	participants, err := r.queries.ListConversationParticipants(ctx, conversation.ID)
	if err != nil {
		return nil, err
	}
	participantNames := make([]string, 0, len(participants))
	for _, participant := range participants {
		name := strings.TrimSpace(participant.DisplayName)
		if name == "" {
			name = strings.TrimSpace(participant.Handle)
		}
		if name != "" {
			participantNames = append(participantNames, name)
		}
	}

	var workspaceName string
	var workspaceSlug string
	if conversation.WorkspaceID != nil {
		workspace, err := r.loadWorkspace(ctx, *conversation.WorkspaceID)
		if err == nil {
			workspaceName = workspace.Name
			workspaceSlug = workspace.Slug
		}
	}

	body := strings.Join(nonEmpty(
		stringValue(trimOptionalStringValue(conversation.Description)),
		workspaceName,
		strings.Join(participantNames, " "),
	), "\n")
	documents := make([]searchDocument, 0)
	for _, anchor := range conversationAccessAnchors(conversation) {
		documents = append(documents, searchDocument{
			Kind:             documentKindConversation,
			CanonicalID:      conversation.ID.String(),
			ResultKey:        resultKey(documentKindConversation, conversation.ID.String()),
			DocID:            documentID(documentKindConversation, conversation.ID.String(), anchor),
			WorkspaceID:      anchor.WorkspaceID,
			ConversationID:   &conversation.ID,
			ReadPrincipalIDs: []uuid.UUID{anchor.PrincipalID},
			Title:            stringValue(title),
			Body:             body,
			ExactTerms:       namedDocumentTerms(stringValue(title), workspaceName, workspaceSlug),
			CreatedAt:        conversation.CreatedAt.UTC(),
			UpdatedAt:        conversation.UpdatedAt.UTC(),
			Archived:         conversation.ArchivedAt != nil,
			EmbeddingText:    strings.Join(nonEmpty(stringValue(title), body, workspaceName), "\n"),
		})
	}
	return mergeDocumentsByID(documents), nil
}

func (r *Runtime) buildWorkspaceDocuments(ctx context.Context, workspaceID uuid.UUID) ([]searchDocument, error) {
	workspace, err := r.loadWorkspace(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	anchor := documentAnchor{
		PrincipalID: workspacePrincipalID(workspace.ID),
		WorkspaceID: &workspace.ID,
		AnchorKey:   "workspace:" + workspace.ID.String(),
	}
	document := searchDocument{
		Kind:             documentKindWorkspace,
		CanonicalID:      workspace.ID.String(),
		ResultKey:        resultKey(documentKindWorkspace, workspace.ID.String()),
		DocID:            documentID(documentKindWorkspace, workspace.ID.String(), anchor),
		WorkspaceID:      &workspace.ID,
		ReadPrincipalIDs: []uuid.UUID{anchor.PrincipalID},
		Title:            workspace.Name,
		Body:             workspace.Slug,
		ExactTerms:       namedDocumentTerms(workspace.Name, workspace.Slug),
		CreatedAt:        workspace.CreatedAt.UTC(),
		UpdatedAt:        workspace.UpdatedAt.UTC(),
		EmbeddingText:    strings.Join(nonEmpty(workspace.Name, workspace.Slug), "\n"),
	}
	return []searchDocument{document}, nil
}

func (r *Runtime) buildUserDocuments(ctx context.Context, userID uuid.UUID) ([]searchDocument, error) {
	user, err := r.loadUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if user.Status != "active" {
		return nil, nil
	}

	documents := []searchDocument{{
		Kind:             documentKindUser,
		CanonicalID:      user.ID.String(),
		ResultKey:        resultKey(documentKindUser, user.ID.String()),
		DocID:            documentID(documentKindUser, user.ID.String(), documentAnchor{AnchorKey: "self"}),
		ReadPrincipalIDs: []uuid.UUID{userPrincipalID(user.ID)},
		Title:            user.DisplayName,
		Body:             strings.Join(nonEmpty("@"+user.Handle, stringValue(user.Email), stringValue(user.Bio)), "\n"),
		ExactTerms:       namedDocumentTerms(user.DisplayName, "@"+user.Handle, stringValue(user.Email)),
		CreatedAt:        user.CreatedAt,
		UpdatedAt:        user.UpdatedAt,
		EmbeddingText:    strings.Join(nonEmpty(user.DisplayName, user.Handle, stringValue(user.Email), stringValue(user.Bio)), "\n"),
	}}

	workspaceAnchors, err := r.listActiveWorkspaceAnchorsForUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	for _, anchor := range workspaceAnchors {
		documents = append(documents, searchDocument{
			Kind:             documentKindUser,
			CanonicalID:      user.ID.String(),
			ResultKey:        resultKey(documentKindUser, user.ID.String()),
			DocID:            documentID(documentKindUser, user.ID.String(), anchor),
			WorkspaceID:      anchor.WorkspaceID,
			ReadPrincipalIDs: []uuid.UUID{anchor.PrincipalID},
			Title:            user.DisplayName,
			Body:             strings.Join(nonEmpty("@"+user.Handle, stringValue(user.Email), stringValue(user.Bio)), "\n"),
			ExactTerms:       namedDocumentTerms(user.DisplayName, "@"+user.Handle, stringValue(user.Email)),
			CreatedAt:        user.CreatedAt,
			UpdatedAt:        user.UpdatedAt,
			EmbeddingText:    strings.Join(nonEmpty(user.DisplayName, user.Handle, stringValue(user.Email), stringValue(user.Bio)), "\n"),
		})
	}

	conversationAnchors, err := r.listMemberConversationScopesByUser(ctx, user.ID, nil)
	if err != nil {
		return nil, err
	}
	for _, anchor := range conversationAnchors {
		documents = append(documents, searchDocument{
			Kind:             documentKindUser,
			CanonicalID:      user.ID.String(),
			ResultKey:        resultKey(documentKindUser, user.ID.String()),
			DocID:            documentID(documentKindUser, user.ID.String(), anchor),
			WorkspaceID:      anchor.WorkspaceID,
			ConversationID:   anchor.ConversationID,
			ReadPrincipalIDs: []uuid.UUID{anchor.PrincipalID},
			Title:            user.DisplayName,
			Body:             strings.Join(nonEmpty("@"+user.Handle, stringValue(user.Email), stringValue(user.Bio)), "\n"),
			ExactTerms:       namedDocumentTerms(user.DisplayName, "@"+user.Handle, stringValue(user.Email)),
			CreatedAt:        user.CreatedAt,
			UpdatedAt:        user.UpdatedAt,
			EmbeddingText:    strings.Join(nonEmpty(user.DisplayName, user.Handle, stringValue(user.Email), stringValue(user.Bio)), "\n"),
		})
	}
	return mergeDocumentsByID(documents), nil
}

func (r *Runtime) buildEventDocuments(ctx context.Context, eventID int64) ([]searchDocument, error) {
	event, err := r.loadExternalEventByID(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.buildEventDocumentsFromRow(ctx, event)
}

func (r *Runtime) buildEventDocumentsFromSource(ctx context.Context, sourceInternalEventID int64) ([]searchDocument, error) {
	event, err := r.loadExternalEventBySourceInternalEventID(ctx, sourceInternalEventID)
	if err != nil {
		return nil, err
	}
	return r.buildEventDocumentsFromRow(ctx, event)
}

func (r *Runtime) buildEventDocumentsFromRow(ctx context.Context, event externalEventRow) ([]searchDocument, error) {
	anchors, err := r.listEventAnchors(ctx, event)
	if err != nil {
		return nil, err
	}
	title := strings.ReplaceAll(event.Type, ".", " ")
	body := strings.Join(nonEmpty(event.ResourceType, event.ResourceID.String(), flattenJSONText(event.Payload)), "\n")
	documents := make([]searchDocument, 0, len(anchors))
	for _, anchor := range anchors {
		documents = append(documents, searchDocument{
			Kind:             documentKindEvent,
			CanonicalID:      int64String(event.ID),
			ResultKey:        resultKey(documentKindEvent, int64String(event.ID)),
			DocID:            documentID(documentKindEvent, int64String(event.ID), anchor),
			WorkspaceID:      anchor.WorkspaceID,
			ConversationID:   anchor.ConversationID,
			ReadPrincipalIDs: []uuid.UUID{anchor.PrincipalID},
			Title:            title,
			Body:             body,
			ExactTerms:       namedDocumentTerms(event.Type, event.ResourceType, event.ResourceID.String()),
			CreatedAt:        event.OccurredAt.UTC(),
			UpdatedAt:        event.OccurredAt.UTC(),
			EmbeddingText:    strings.Join(nonEmpty(title, body), "\n"),
		})
	}
	return mergeDocumentsByID(documents), nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
