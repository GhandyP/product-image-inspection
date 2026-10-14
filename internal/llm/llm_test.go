package llm

import (
	"context"
	"errors"
	"testing"
)

func TestNameHonoursTheEnvironment(t *testing.T) {
	t.Setenv("ADK_MODEL", "")
	if got := Name(); got != DefaultModel {
		t.Errorf("Name() = %q, want %q", got, DefaultModel)
	}

	t.Setenv("ADK_MODEL", "  gemini-2.5-pro  ")
	if got := Name(); got != "gemini-2.5-pro" {
		t.Errorf("Name() = %q, want the override without its padding", got)
	}

	t.Setenv("ADK_MODEL", "   ")
	if got := Name(); got != DefaultModel {
		t.Errorf("Name() = %q, want the default when the override is blank", got)
	}
}

func TestNewNeedsAKeyAndNothingElse(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "   ")

	if _, err := New(context.Background()); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("New() error = %v, want %v", err, ErrMissingAPIKey)
	}
}
