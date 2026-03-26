package postgres

import (
	"context"
	"testing"

	"github.com/suhjohn/teraslack/internal/domain"
)

func TestWorkspaceRepo_Create_DefaultsNilDefaultChannelsToEmptyArray(t *testing.T) {
	t.Parallel()

	db := &captureDB{}
	repo := NewWorkspaceRepo(db)

	_, err := repo.Create(context.Background(), domain.CreateWorkspaceParams{
		Name: "Acme",
	})
	if err == nil {
		t.Fatal("expected scan error from capture row")
	}
	if len(db.args) < 10 {
		t.Fatalf("args = %#v, want at least 10 values", db.args)
	}

	channels, ok := db.args[9].([]string)
	if !ok {
		t.Fatalf("default_channels arg type = %T, want []string", db.args[9])
	}
	if channels == nil {
		t.Fatal("default_channels arg should not be nil")
	}
	if len(channels) != 0 {
		t.Fatalf("default_channels arg = %#v, want empty slice", channels)
	}
}
