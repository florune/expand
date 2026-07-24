package profile

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound    = errors.New("local profile not found")
	ErrNameExists  = errors.New("a local profile with this name already exists")
	ErrInvalidName = errors.New("profile name is required")
	ErrInvalidID   = errors.New("invalid profile id")
)

type Profile struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"createdAt"`
	LastUsedAt string `json:"lastUsedAt"`
}

type indexFile struct {
	Version  int       `json:"version"`
	Profiles []Profile `json:"profiles"`
}

type Manager struct {
	root  string
	index string
	mu    sync.Mutex
}

func New(root string) *Manager {
	return &Manager{root: root, index: filepath.Join(root, "profiles.json")}
}

func (m *Manager) List() ([]Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	index, err := m.loadLocked()
	if err != nil {
		return nil, err
	}
	items := append(make([]Profile, 0, len(index.Profiles)), index.Profiles...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].LastUsedAt == items[j].LastUsedAt {
			return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		}
		return items[i].LastUsedAt > items[j].LastUsedAt
	})
	return items, nil
}

func (m *Manager) Create(name string) (Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Profile{}, ErrInvalidName
	}
	if len([]rune(name)) > 40 {
		return Profile{}, errors.New("profile name must not exceed 40 characters")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	index, err := m.loadLocked()
	if err != nil {
		return Profile{}, err
	}
	for _, item := range index.Profiles {
		if strings.EqualFold(item.Name, name) {
			return Profile{}, ErrNameExists
		}
	}
	id, err := newID()
	if err != nil {
		return Profile{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	item := Profile{ID: id, Name: name, CreatedAt: now, LastUsedAt: now}
	index.Profiles = append(index.Profiles, item)
	if err := m.persistLocked(index); err != nil {
		return Profile{}, err
	}
	if err := os.MkdirAll(m.ProfileDir(id), 0o700); err != nil {
		return Profile{}, fmt.Errorf("create profile directory: %w", err)
	}
	return item, nil
}

func (m *Manager) Get(id string) (Profile, error) {
	if !validID(id) {
		return Profile{}, ErrInvalidID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	index, err := m.loadLocked()
	if err != nil {
		return Profile{}, err
	}
	for _, item := range index.Profiles {
		if item.ID == id {
			return item, nil
		}
	}
	return Profile{}, ErrNotFound
}

func (m *Manager) Touch(id string) (Profile, error) {
	if !validID(id) {
		return Profile{}, ErrInvalidID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	index, err := m.loadLocked()
	if err != nil {
		return Profile{}, err
	}
	for i := range index.Profiles {
		if index.Profiles[i].ID == id {
			index.Profiles[i].LastUsedAt = time.Now().UTC().Format(time.RFC3339)
			if err := m.persistLocked(index); err != nil {
				return Profile{}, err
			}
			return index.Profiles[i], nil
		}
	}
	return Profile{}, ErrNotFound
}

func (m *Manager) VaultPath(id string) string {
	if !validID(id) {
		return ""
	}
	return filepath.Join(m.ProfileDir(id), "vault.enc")
}

func (m *Manager) ProfileDir(id string) string {
	if !validID(id) {
		return ""
	}
	return filepath.Join(m.root, "profiles", id)
}

func (m *Manager) loadLocked() (indexFile, error) {
	raw, err := os.ReadFile(m.index)
	if errors.Is(err, os.ErrNotExist) {
		return indexFile{Version: 1, Profiles: []Profile{}}, nil
	}
	if err != nil {
		return indexFile{}, fmt.Errorf("read profile index: %w", err)
	}
	var index indexFile
	if err := json.Unmarshal(raw, &index); err != nil || index.Version != 1 {
		return indexFile{}, errors.New("profile index is damaged")
	}
	return index, nil
}

func (m *Manager) persistLocked(index indexFile) error {
	raw, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.root, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(m.root, ".profiles-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, m.index)
}

func newID() (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func validID(id string) bool {
	if len(id) != 24 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
