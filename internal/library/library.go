package library

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/florune/expand/internal/model"
	"gopkg.in/yaml.v3"
)

var (
	ErrNotFound         = errors.New("entry not found")
	ErrDuplicateID      = errors.New("duplicate entry id")
	ErrDuplicateTrigger = errors.New("duplicate trigger")
)

type Store struct {
	dir     string
	mu      sync.RWMutex
	entries map[string]model.Entry
}

func New(dir string) *Store {
	return &Store{dir: dir, entries: make(map[string]model.Entry)}
}

func (s *Store) Dir() string { return s.dir }

func (s *Store) Load() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create library directory: %w", err)
	}

	paths := make([]string, 0)
	err := filepath.WalkDir(s.dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yml" || ext == ".yaml" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("scan library: %w", err)
	}
	sort.Strings(paths)

	loaded := make(map[string]model.Entry)
	triggers := make(map[string]string)
	for _, path := range paths {
		doc, err := readDocument(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.dir, path)
		for i := range doc.Entries {
			item := normalize(doc.Entries[i])
			if err := validate(item); err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
			if previous, ok := loaded[item.ID]; ok {
				return fmt.Errorf("%w %q in %s and %s", ErrDuplicateID, item.ID, previous.SourceFile, rel)
			}
			if previous, ok := triggers[item.Trigger]; ok {
				return fmt.Errorf("%w %q in %s and %s", ErrDuplicateTrigger, item.Trigger, previous, rel)
			}
			item.SourceFile = filepath.ToSlash(rel)
			loaded[item.ID] = item
			triggers[item.Trigger] = item.SourceFile
		}
	}

	s.mu.Lock()
	s.entries = loaded
	s.mu.Unlock()
	return nil
}

func (s *Store) List() []model.Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]model.Entry, 0, len(s.entries))
	for _, item := range s.entries {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Favorite != items[j].Favorite {
			return items[i].Favorite
		}
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].Trigger < items[j].Trigger
	})
	return items
}

func (s *Store) Get(id string) (model.Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.entries[id]
	if !ok {
		return model.Entry{}, ErrNotFound
	}
	return item, nil
}

func (s *Store) Search(query, category string) []model.Entry {
	query = strings.ToLower(strings.TrimSpace(query))
	category = strings.ToLower(strings.TrimSpace(category))
	tokens := strings.Fields(query)

	type scored struct {
		item  model.Entry
		score int
	}
	result := make([]scored, 0)
	for _, item := range s.List() {
		if category != "" && category != "all" && strings.ToLower(item.Category) != category {
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{
			item.Trigger, item.Title, item.Description, item.Category,
			strings.Join(item.Tags, " "), item.Project, item.Environment,
		}, " "))
		score := 0
		matched := true
		for _, token := range tokens {
			if !strings.Contains(haystack, token) {
				matched = false
				break
			}
			score += 10
			if strings.HasPrefix(strings.ToLower(item.Trigger), token) {
				score += 20
			}
			if strings.Contains(strings.ToLower(item.Title), token) {
				score += 8
			}
		}
		if matched {
			if item.Favorite {
				score += 5
			}
			result = append(result, scored{item: item, score: score})
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].score != result[j].score {
			return result[i].score > result[j].score
		}
		return result[i].item.Trigger < result[j].item.Trigger
	})
	items := make([]model.Entry, len(result))
	for i := range result {
		items[i] = result[i].item
	}
	return items
}

func (s *Store) Save(item model.Entry) (model.Entry, error) {
	item = normalize(item)
	if item.ID == "" {
		item.ID = newID("entry")
	}
	item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := validate(item); err != nil {
		return model.Entry{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for id, existing := range s.entries {
		if id != item.ID && existing.Trigger == item.Trigger {
			return model.Entry{}, fmt.Errorf("%w %q", ErrDuplicateTrigger, item.Trigger)
		}
	}

	if existing, ok := s.entries[item.ID]; ok {
		item.SourceFile = existing.SourceFile
	} else {
		item.SourceFile = "user.yml"
	}
	if err := s.writeFileLocked(item.SourceFile, item, false); err != nil {
		return model.Entry{}, err
	}
	s.entries[item.ID] = item
	return item, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.entries[id]
	if !ok {
		return ErrNotFound
	}
	if err := s.writeFileLocked(item.SourceFile, item, true); err != nil {
		return err
	}
	delete(s.entries, id)
	return nil
}

func (s *Store) writeFileLocked(source string, changed model.Entry, remove bool) error {
	path := filepath.Join(s.dir, filepath.FromSlash(source))
	doc := model.Document{Version: 1}
	if _, err := os.Stat(path); err == nil {
		loaded, readErr := readDocument(path)
		if readErr != nil {
			return readErr
		}
		doc = loaded
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	found := false
	entries := make([]model.Entry, 0, len(doc.Entries)+1)
	for _, item := range doc.Entries {
		if item.ID == changed.ID {
			found = true
			if !remove {
				changed.SourceFile = ""
				entries = append(entries, changed)
			}
			continue
		}
		item.SourceFile = ""
		entries = append(entries, item)
	}
	if !remove && !found {
		changed.SourceFile = ""
		entries = append(entries, changed)
	}
	doc.Version = 1
	doc.Entries = entries
	return writeDocument(path, doc)
}

func readDocument(path string) (model.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Document{}, fmt.Errorf("read %s: %w", path, err)
	}
	var doc model.Document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return model.Document{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc.Version == 0 {
		doc.Version = 1
	}
	return doc, nil
}

func writeDocument(path string, doc model.Document) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".expand-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func normalize(item model.Entry) model.Entry {
	item.ID = strings.TrimSpace(item.ID)
	item.Trigger = strings.TrimSpace(item.Trigger)
	item.Title = strings.TrimSpace(item.Title)
	item.Category = strings.TrimSpace(item.Category)
	item.RiskLevel = strings.ToLower(strings.TrimSpace(item.RiskLevel))
	if item.RiskLevel == "" {
		item.RiskLevel = "safe"
	}
	return item
}

func validate(item model.Entry) error {
	if item.ID == "" {
		return errors.New("id is required")
	}
	if item.Trigger == "" || !strings.HasPrefix(item.Trigger, ":") {
		return errors.New("trigger must start with ':'")
	}
	if strings.ContainsAny(item.Trigger, " \t\r\n") {
		return errors.New("trigger cannot contain whitespace")
	}
	if item.Title == "" {
		return errors.New("title is required")
	}
	if item.Category == "" {
		return errors.New("category is required")
	}
	if item.Template == "" {
		return errors.New("template is required")
	}
	return nil
}

func newID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(buf)
}
