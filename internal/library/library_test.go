package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/florune/expand/internal/model"
)

func TestStoreLoadSearchSaveDelete(t *testing.T) {
	dir := t.TempDir()
	doc := `version: 1
entries:
  - id: kafka-group
    trigger: :kafka_describe_group
    title: Describe Kafka group
    category: kafka
    template: kafka {{group}}
    tags: [offset, lag]
`
	if err := os.WriteFile(filepath.Join(dir, "kafka.yml"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	store := New(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if got := store.Search("offset", "kafka"); len(got) != 1 || got[0].ID != "kafka-group" {
		t.Fatalf("unexpected search result: %#v", got)
	}

	saved, err := store.Save(model.Entry{
		Trigger: ":today", Title: "Today", Category: "common", Template: "2026-07-24",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" || saved.SourceFile != "user.yml" {
		t.Fatalf("unexpected saved entry: %#v", saved)
	}
	if err := store.Delete(saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(saved.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestStoreRejectsDuplicateTriggers(t *testing.T) {
	dir := t.TempDir()
	doc := `version: 1
entries:
  - {id: one, trigger: ':same', title: One, category: common, type: text, template: one}
  - {id: two, trigger: ':same', title: Two, category: common, type: text, template: two}
`
	if err := os.WriteFile(filepath.Join(dir, "duplicates.yml"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	err := New(dir).Load()
	if !errors.Is(err, ErrDuplicateTrigger) {
		t.Fatalf("expected duplicate trigger error, got %v", err)
	}
}

func TestStoreForcesNewEntriesIntoUserFile(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save(model.Entry{
		ID: "safe-path", Trigger: ":safe_path", Title: "Safe path", Category: "common",
		Template: "safe", SourceFile: "../../escape.yml",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.SourceFile != "user.yml" {
		t.Fatalf("new entry escaped the managed file: %q", saved.SourceFile)
	}
	if _, err := os.Stat(filepath.Join(dir, "user.yml")); err != nil {
		t.Fatalf("user file was not written: %v", err)
	}
}
