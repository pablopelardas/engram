package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnvFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestLoadEnvFile_BasicParse(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, "test.env", `
# A comment
INTUIT_ENGRAM_TEST_FOO=bar
INTUIT_ENGRAM_TEST_QUOTED="hello world"
INTUIT_ENGRAM_TEST_SINGLE='single quoted'

INTUIT_ENGRAM_TEST_TRIM   =   trimmed
`)
	t.Setenv(envFileOverrideVar, path)
	// Make sure target keys are clean.
	for _, k := range []string{
		"INTUIT_ENGRAM_TEST_FOO", "INTUIT_ENGRAM_TEST_QUOTED",
		"INTUIT_ENGRAM_TEST_SINGLE", "INTUIT_ENGRAM_TEST_TRIM",
	} {
		_ = os.Unsetenv(k)
	}

	loadEnvFile()

	cases := map[string]string{
		"INTUIT_ENGRAM_TEST_FOO":    "bar",
		"INTUIT_ENGRAM_TEST_QUOTED": "hello world",
		"INTUIT_ENGRAM_TEST_SINGLE": "single quoted",
		"INTUIT_ENGRAM_TEST_TRIM":   "trimmed",
	}
	for k, want := range cases {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s: expected %q, got %q", k, want, got)
		}
		_ = os.Unsetenv(k)
	}
}

func TestLoadEnvFile_DoesNotOverrideExistingEnv(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, "x.env", "INTUIT_ENGRAM_TEST_PRESET=from_file\n")
	t.Setenv(envFileOverrideVar, path)
	t.Setenv("INTUIT_ENGRAM_TEST_PRESET", "from_shell")

	loadEnvFile()

	if got := os.Getenv("INTUIT_ENGRAM_TEST_PRESET"); got != "from_shell" {
		t.Fatalf("file should not override shell: got %q", got)
	}
}

func TestLoadEnvFile_OverridePointsAtMissingFile(t *testing.T) {
	// When the override is set but points at nothing, we warn but keep going.
	t.Setenv(envFileOverrideVar, filepath.Join(t.TempDir(), "does-not-exist.env"))
	loadEnvFile() // must not panic
}

func TestLoadEnvFile_BadLines(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, "bad.env", "no_equals_here\nKEY=value\n=novalue\n")
	t.Setenv(envFileOverrideVar, path)
	_ = os.Unsetenv("KEY")

	loadEnvFile() // should warn but not crash; KEY should still be set

	if got := os.Getenv("KEY"); got != "value" {
		t.Fatalf("KEY should be set despite earlier malformed lines, got %q", got)
	}
}
