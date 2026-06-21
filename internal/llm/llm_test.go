package llm

import "testing"

func TestNewRequiresKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	if _, err := New(""); err == nil {
		t.Errorf("expected error when OPENROUTER_API_KEY is unset")
	}
}

func TestNewFallbackChain(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("OPENROUTER_MODEL", "")
	p, err := New("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.models) < 2 {
		t.Errorf("expected fallback chain when no model chosen, got %v", p.models)
	}
	if p.Model() != DefaultModel {
		t.Errorf("primary model should be DefaultModel, got %q", p.Model())
	}
}

func TestNewExplicitModelNoFallback(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	p, err := New("some/model:free")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.models) != 1 || p.models[0] != "some/model:free" {
		t.Errorf("explicit model should disable fallback, got %v", p.models)
	}
}

func TestNewModelFromEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("OPENROUTER_MODEL", "env/model:free")
	p, _ := New("")
	if p.Model() != "env/model:free" {
		t.Errorf("should use OPENROUTER_MODEL, got %q", p.Model())
	}
}
