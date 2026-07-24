package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/florune/expand/internal/model"
)

func TestVaultRoundTripAndWrongPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.enc")
	v := New(path, time.Minute)
	v.SetKDFParamsForTest(1, 8*1024, 1)
	if err := v.Create("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	saved, err := v.Save(Secret{Name: "MySQL prod", Username: "ops", Value: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("s3cret")) {
		t.Fatal("vault file contains a plaintext secret")
	}
	v.Lock()
	if _, err := v.Value(saved.ID); !errors.Is(err, ErrLocked) {
		t.Fatalf("expected locked error, got %v", err)
	}
	if err := v.Unlock("wrong password"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("expected invalid password, got %v", err)
	}
	if err := v.Unlock("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	value, err := v.Value(saved.ID)
	if err != nil || value != "s3cret" {
		t.Fatalf("unexpected secret value %q, %v", value, err)
	}
	items, err := v.List()
	if err != nil || len(items) != 1 {
		t.Fatalf("unexpected metadata: %#v, %v", items, err)
	}
}

func TestShortcutIsEncryptedAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.enc")
	v := New(path, time.Minute)
	v.SetKDFParamsForTest(1, 8*1024, 1)
	if err := v.Create("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	saved, err := v.SaveShortcut(Shortcut{
		Trigger:  ":mysql-connect-prod",
		Title:    "MySQL prod",
		Template: "mysql --host={{MYSQL_HOST}} --user={{MYSQL_USER}} -p",
		Variables: []model.Variable{
			{Name: "MYSQL_HOST", Label: "主机", Default: "MYSQL_HOST"},
			{Name: "MYSQL_USER", Label: "用户", Default: "MYSQL_USER"},
		},
		Fields: map[string]string{
			"MYSQL_HOST": "db-prod.internal",
			"MYSQL_USER": "developer",
		},
		Content: "mysql --host=db-prod.internal -p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" {
		t.Fatal("shortcut id was not generated")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("db-prod.internal")) ||
		bytes.Contains(raw, []byte(":mysql-connect-prod")) ||
		bytes.Contains(raw, []byte("MYSQL_HOST")) {
		t.Fatal("vault contains plaintext shortcut data")
	}
	v.Lock()
	if err := v.Unlock("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	found, err := v.FindShortcut(":mysql-connect-prod")
	if err != nil {
		t.Fatal(err)
	}
	if found.Content != "mysql --host=db-prod.internal -p" {
		t.Fatalf("unexpected content %q", found.Content)
	}
	if found.Template != "mysql --host={{MYSQL_HOST}} --user={{MYSQL_USER}} -p" {
		t.Fatalf("unexpected template %q", found.Template)
	}
	if len(found.Variables) != 2 || found.Fields["MYSQL_USER"] != "developer" {
		t.Fatalf("template configuration did not persist: %#v %#v", found.Variables, found.Fields)
	}
}

func TestEmptyShortcutListIsNotNil(t *testing.T) {
	v := New(filepath.Join(t.TempDir(), "vault.enc"), time.Minute)
	v.SetKDFParamsForTest(1, 8*1024, 1)
	if err := v.Create("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	items, err := v.ListShortcuts()
	if err != nil {
		t.Fatal(err)
	}
	if items == nil {
		t.Fatal("empty shortcut list must encode as [] rather than null")
	}
}

func TestVaultAutoLock(t *testing.T) {
	v := New(filepath.Join(t.TempDir(), "vault.enc"), 10*time.Millisecond)
	v.SetKDFParamsForTest(1, 8*1024, 1)
	if err := v.Create("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if v.Status().Unlocked {
		t.Fatal("vault should auto-lock")
	}
}
