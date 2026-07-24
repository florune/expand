package template

import (
	"strings"
	"testing"
	"time"

	"github.com/florune/expand/internal/model"
)

func TestRender(t *testing.T) {
	entry := model.Entry{
		Template: "ssh {{user}}@{{host}}",
		Variables: []model.Variable{
			{Name: "user", Label: "User", Default: "root"},
			{Name: "host", Label: "Host", Required: true},
		},
	}
	got, err := Render(entry, map[string]string{"host": "server.example"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "ssh root@server.example" {
		t.Fatalf("unexpected render: %q", got)
	}
	if _, err := Render(entry, map[string]string{}); err == nil || !strings.Contains(err.Error(), "Host") {
		t.Fatalf("expected required value error, got %v", err)
	}
}

func TestRenderDateVariable(t *testing.T) {
	entry := model.Entry{
		Template:  "{{today}}",
		Variables: []model.Variable{{Name: "today", Label: "Today", Type: "date", Format: "2006-01-02"}},
	}
	got, err := RenderAt(entry, nil, time.Date(2026, 7, 24, 10, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if got != "2026-07-24" {
		t.Fatalf("unexpected date: %q", got)
	}
}

func TestVariablesDiscoversPlaceholdersAndPreservesMetadata(t *testing.T) {
	variables := Variables(
		"mysql --user={{MYSQL_USER}} --host={{MYSQL_HOST}} --user={{MYSQL_USER}}",
		[]model.Variable{{Name: "MYSQL_HOST", Label: "主机", Default: "127.0.0.1"}},
	)
	if len(variables) != 2 {
		t.Fatalf("expected two unique variables, got %#v", variables)
	}
	if variables[0].Name != "MYSQL_USER" || variables[0].Default != "MYSQL_USER" {
		t.Fatalf("unexpected discovered variable: %#v", variables[0])
	}
	if variables[1].Label != "主机" || variables[1].Default != "127.0.0.1" {
		t.Fatalf("declared metadata was not preserved: %#v", variables[1])
	}
}

func TestRenderUndeclaredVariableUsesVisiblePlaceholder(t *testing.T) {
	got, err := Render(model.Entry{Template: "ssh {{SSH_USER}}@{{SSH_HOST}}"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ssh SSH_USER@SSH_HOST" {
		t.Fatalf("unexpected fallback render: %q", got)
	}
}
