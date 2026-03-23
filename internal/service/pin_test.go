package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/suhjohn/teraslack/internal/ctxutil"
	"github.com/suhjohn/teraslack/internal/domain"
	"github.com/suhjohn/teraslack/internal/repository"
)

type mockPinRepoTenant struct{}

func (m *mockPinRepoTenant) Add(_ context.Context, params domain.PinParams) (*domain.Pin, error) {
	return &domain.Pin{ChannelID: params.ChannelID, MessageTS: params.MessageTS, PinnedBy: params.UserID}, nil
}
func (m *mockPinRepoTenant) Remove(_ context.Context, _ domain.PinParams) error { return nil }
func (m *mockPinRepoTenant) List(_ context.Context, _ domain.ListPinsParams) ([]domain.Pin, error) {
	return []domain.Pin{}, nil
}
func (m *mockPinRepoTenant) WithTx(_ pgx.Tx) repository.PinRepository { return m }

type mockConvRepoForPinTenant struct {
	teamID string
}

func (m *mockConvRepoForPinTenant) Create(_ context.Context, _ domain.CreateConversationParams) (*domain.Conversation, error) {
	return nil, nil
}
func (m *mockConvRepoForPinTenant) Get(_ context.Context, id string) (*domain.Conversation, error) {
	if id == "" {
		return nil, domain.ErrNotFound
	}
	return &domain.Conversation{ID: id, TeamID: m.teamID}, nil
}
func (m *mockConvRepoForPinTenant) Update(_ context.Context, _ string, _ domain.UpdateConversationParams) (*domain.Conversation, error) {
	return nil, nil
}
func (m *mockConvRepoForPinTenant) SetTopic(_ context.Context, _ string, _ domain.SetTopicParams) (*domain.Conversation, error) {
	return nil, nil
}
func (m *mockConvRepoForPinTenant) SetPurpose(_ context.Context, _ string, _ domain.SetPurposeParams) (*domain.Conversation, error) {
	return nil, nil
}
func (m *mockConvRepoForPinTenant) List(_ context.Context, _ domain.ListConversationsParams) (*domain.CursorPage[domain.Conversation], error) {
	return nil, nil
}
func (m *mockConvRepoForPinTenant) Archive(_ context.Context, _ string) error   { return nil }
func (m *mockConvRepoForPinTenant) Unarchive(_ context.Context, _ string) error { return nil }
func (m *mockConvRepoForPinTenant) AddMember(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockConvRepoForPinTenant) RemoveMember(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockConvRepoForPinTenant) ListMembers(_ context.Context, _ string, _ string, _ int) (*domain.CursorPage[domain.ConversationMember], error) {
	return nil, nil
}
func (m *mockConvRepoForPinTenant) IsMember(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}
func (m *mockConvRepoForPinTenant) WithTx(_ pgx.Tx) repository.ConversationRepository { return m }

type mockMsgRepoForPinTenant struct{}

func (m *mockMsgRepoForPinTenant) Create(_ context.Context, _ domain.PostMessageParams) (*domain.Message, error) {
	return nil, nil
}
func (m *mockMsgRepoForPinTenant) Get(_ context.Context, channelID, ts string) (*domain.Message, error) {
	return &domain.Message{ChannelID: channelID, TS: ts}, nil
}
func (m *mockMsgRepoForPinTenant) Update(_ context.Context, _, _ string, _ domain.UpdateMessageParams) (*domain.Message, error) {
	return nil, nil
}
func (m *mockMsgRepoForPinTenant) Delete(_ context.Context, _, _ string) error { return nil }
func (m *mockMsgRepoForPinTenant) ListHistory(_ context.Context, _ domain.ListMessagesParams) (*domain.CursorPage[domain.Message], error) {
	return nil, nil
}
func (m *mockMsgRepoForPinTenant) ListReplies(_ context.Context, _ domain.ListRepliesParams) (*domain.CursorPage[domain.Message], error) {
	return nil, nil
}
func (m *mockMsgRepoForPinTenant) AddReaction(_ context.Context, _ domain.AddReactionParams) error {
	return nil
}
func (m *mockMsgRepoForPinTenant) RemoveReaction(_ context.Context, _ domain.RemoveReactionParams) error {
	return nil
}
func (m *mockMsgRepoForPinTenant) GetReactions(_ context.Context, _, _ string) ([]domain.Reaction, error) {
	return nil, nil
}
func (m *mockMsgRepoForPinTenant) WithTx(_ pgx.Tx) repository.MessageRepository { return m }

func TestPinService_TenantAccessDenied(t *testing.T) {
	svc := NewPinService(&mockPinRepoTenant{}, &mockConvRepoForPinTenant{teamID: "T999"}, &mockMsgRepoForPinTenant{}, nil, mockTxBeginner{}, nil)

	ctx := context.WithValue(context.Background(), ctxutil.ContextKeyTeamID, "T123")

	if _, err := svc.Add(ctx, domain.PinParams{ChannelID: "C123", MessageTS: "123.456", UserID: "U123"}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from Add, got %v", err)
	}
	if err := svc.Remove(ctx, domain.PinParams{ChannelID: "C123", MessageTS: "123.456"}); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from Remove, got %v", err)
	}
	if _, err := svc.List(ctx, "C123"); err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden from List, got %v", err)
	}
}
