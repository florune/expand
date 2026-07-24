package profile

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestProfilesAreListedAndHaveSeparateVaultPaths(t *testing.T) {
	manager := New(t.TempDir())
	first, err := manager.Create("alice")
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Create("bob")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("profiles must have different ids")
	}
	if manager.VaultPath(first.ID) == manager.VaultPath(second.ID) {
		t.Fatal("profiles must have separate vault files")
	}
	if filepath.Base(manager.VaultPath(first.ID)) != "vault.enc" {
		t.Fatalf("unexpected vault path %q", manager.VaultPath(first.ID))
	}
	items, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(items))
	}
}

func TestProfileNamesAreUniqueCaseInsensitive(t *testing.T) {
	manager := New(t.TempDir())
	if _, err := manager.Create("Florune"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create("florune"); !errors.Is(err, ErrNameExists) {
		t.Fatalf("expected duplicate name error, got %v", err)
	}
}

func TestEmptyProfileListIsNotNil(t *testing.T) {
	items, err := New(t.TempDir()).List()
	if err != nil {
		t.Fatal(err)
	}
	if items == nil {
		t.Fatal("empty profile list must encode as [] rather than null")
	}
}
