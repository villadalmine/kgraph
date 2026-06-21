package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultOut(t *testing.T) {
	if got := defaultOut("", "pihole", "svg"); got != "pihole.svg" {
		t.Errorf("defaultOut default = %q", got)
	}
	if got := defaultOut("custom.d2", "pihole", "svg"); got != "custom.d2" {
		t.Errorf("explicit output should win, got %q", got)
	}
}

// loadDotEnv loads KEY=VALUE without overriding existing env, supports export/
// comments/quotes (spec 0008).
func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# comment\nexport FOO=bar\nQUOTED=\"baz qux\"\nPREEXISTING=fromfile\n\nMALFORMED\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PREEXISTING", "fromenv")
	// Ensure our keys are clean before loading.
	os.Unsetenv("FOO")
	os.Unsetenv("QUOTED")

	loadDotEnv(path)

	if got := os.Getenv("FOO"); got != "bar" {
		t.Errorf("FOO = %q, want bar (export prefix + value)", got)
	}
	if got := os.Getenv("QUOTED"); got != "baz qux" {
		t.Errorf("QUOTED = %q, want 'baz qux' (quotes stripped)", got)
	}
	if got := os.Getenv("PREEXISTING"); got != "fromenv" {
		t.Errorf("PREEXISTING = %q, real env must win", got)
	}
}

func TestLoadDotEnvMissingFileIsNoop(t *testing.T) {
	loadDotEnv(filepath.Join(t.TempDir(), "does-not-exist"))
}
