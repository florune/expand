package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/florune/expand/internal/library"
	"github.com/florune/expand/internal/model"
	templater "github.com/florune/expand/internal/template"
	"github.com/florune/expand/internal/vault"
)

func TestNormaliseMySQLShortcutBuildsPasswordPromptCommand(t *testing.T) {
	item, err := normaliseShortcut(vault.Shortcut{
		Trigger: ":mysql-connect-prod",
		Title:   "MySQL prod",
		Kind:    "mysql",
		Fields: map[string]string{
			"host": "db-prod.internal", "port": "3306",
			"username": "developer", "database": "orders",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(item.Content), "password") {
		t.Fatalf("generated command must not contain a password: %q", item.Content)
	}
	want := "mysql --host=db-prod.internal --port=3306 --user=developer -p orders"
	if item.Content != want {
		t.Fatalf("got %q, want %q", item.Content, want)
	}
}

func TestNormaliseMySQLShortcutRejectsShellMetacharacters(t *testing.T) {
	_, err := normaliseShortcut(vault.Shortcut{
		Kind: "mysql",
		Fields: map[string]string{
			"host": "db.internal && whoami", "port": "3306",
			"username": "developer", "database": "orders",
		},
	})
	if err == nil {
		t.Fatal("expected unsafe host to be rejected")
	}
}

func TestNormaliseShortcutDiscoversAndRendersGenericVariables(t *testing.T) {
	item, err := normaliseShortcut(vault.Shortcut{
		Trigger:  ":ssh-prod",
		Title:    "SSH prod",
		Category: "linux",
		Template: "ssh {{SSH_USER}}@{{SSH_HOST}}",
		Fields: map[string]string{
			"SSH_HOST": "prod.internal",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Kind != "" {
		t.Fatalf("user-facing type should be removed, got %q", item.Kind)
	}
	if len(item.Variables) != 2 || item.Variables[0].Name != "SSH_USER" || item.Variables[1].Name != "SSH_HOST" {
		t.Fatalf("unexpected discovered variables: %#v", item.Variables)
	}
	if item.Content != "ssh SSH_USER@prod.internal" {
		t.Fatalf("unexpected rendered content %q", item.Content)
	}
}

func TestRenderShortcutUsesSavedTemplateValues(t *testing.T) {
	output, err := renderShortcut(vault.Shortcut{
		Template: "mysql -h {{MYSQL_HOST}} -u {{MYSQL_USER}} -p",
		Variables: []model.Variable{
			{Name: "MYSQL_HOST", Default: "MYSQL_HOST"},
			{Name: "MYSQL_USER", Default: "MYSQL_USER"},
		},
		Fields: map[string]string{
			"MYSQL_HOST": "db.internal",
			"MYSQL_USER": "developer",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if output != "mysql -h db.internal -u developer -p" {
		t.Fatalf("unexpected rendered output %q", output)
	}
}

func TestAllBuiltInTemplatesRenderWithoutConfiguration(t *testing.T) {
	store := library.New(filepath.Join(".", "data"))
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	entries := store.List()
	if len(entries) < 30 {
		t.Fatalf("expected a useful starter library, got %d entries", len(entries))
	}
	for _, entry := range entries {
		output, err := templater.Render(entry, nil)
		if err != nil {
			t.Errorf("%s (%s) does not render with defaults: %v", entry.Trigger, entry.SourceFile, err)
			continue
		}
		if strings.TrimSpace(output) == "" {
			t.Errorf("%s rendered empty output", entry.Trigger)
		}
	}
}

func TestEmbeddedTemplatesInstallAndRemoveObsoleteFiles(t *testing.T) {
	dir := t.TempDir()
	builtinDir := filepath.Join(dir, "_builtin")
	if err := os.MkdirAll(builtinDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(builtinDir, "obsolete.yml")
	if err := os.WriteFile(stale, []byte("version: 1\nentries: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syncBuiltinTemplates(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("obsolete built-in template was not removed: %v", err)
	}
	store := library.New(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if len(store.List()) < 40 {
		t.Fatalf("embedded starter library is incomplete: %d entries", len(store.List()))
	}
}

func TestDefaultVaultSessionTTL(t *testing.T) {
	if defaultVaultTTL != 24*time.Hour {
		t.Fatalf("expected a 24-hour vault session, got %s", defaultVaultTTL)
	}
}
