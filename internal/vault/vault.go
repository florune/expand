package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/florune/expand/internal/model"
	"golang.org/x/crypto/argon2"
)

var (
	ErrLocked           = errors.New("vault is locked")
	ErrAlreadyExists    = errors.New("vault already exists")
	ErrInvalidPassword  = errors.New("invalid master password or damaged vault")
	ErrSecretNotFound   = errors.New("secret not found")
	ErrShortcutNotFound = errors.New("shortcut not found")
)

const (
	keyAAD       = "expand-vault-key-v1"
	dataAAD      = "expand-vault-data-v1"
	maxVaultSize = 32 << 20
)

type KDFParams struct {
	Name    string `json:"name"`
	Salt    string `json:"salt"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
}

type envelope struct {
	Version    int       `json:"version"`
	KDF        KDFParams `json:"kdf"`
	KeyNonce   string    `json:"keyNonce"`
	WrappedKey string    `json:"wrappedKey"`
	DataNonce  string    `json:"dataNonce"`
	Ciphertext string    `json:"ciphertext"`
	UpdatedAt  string    `json:"updatedAt"`
}

type Secret struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Username  string   `json:"username,omitempty"`
	Value     string   `json:"value"`
	Notes     string   `json:"notes,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	UpdatedAt string   `json:"updatedAt"`
}

type SecretMeta struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Username  string   `json:"username,omitempty"`
	Notes     string   `json:"notes,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	UpdatedAt string   `json:"updatedAt"`
}

type Shortcut struct {
	ID        string            `json:"id"`
	Trigger   string            `json:"trigger"`
	Title     string            `json:"title"`
	Category  string            `json:"category,omitempty"`
	Kind      string            `json:"kind,omitempty"` // legacy compatibility; new shortcuts use Template.
	Template  string            `json:"template,omitempty"`
	Variables []model.Variable  `json:"variables,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
	Content   string            `json:"content"`
	SecretID  string            `json:"secretId,omitempty"`
	Sensitive bool              `json:"sensitive,omitempty"`
	UpdatedAt string            `json:"updatedAt"`
}

type payload struct {
	Version   int        `json:"version"`
	Secrets   []Secret   `json:"secrets"`
	Shortcuts []Shortcut `json:"shortcuts,omitempty"`
}

type Status struct {
	Exists              bool `json:"exists"`
	Unlocked            bool `json:"unlocked"`
	AutoLockSeconds     int  `json:"autoLockSeconds"`
	RemainingSeconds    int  `json:"remainingSeconds"`
	StoredSecretCount   int  `json:"storedSecretCount"`
	StoredShortcutCount int  `json:"storedShortcutCount"`
}

type Vault struct {
	path     string
	timeout  time.Duration
	params   KDFParams
	mu       sync.Mutex
	env      *envelope
	data     *payload
	dataKey  []byte
	lastUsed time.Time
}

func New(path string, timeout time.Duration) *Vault {
	return &Vault{
		path:    path,
		timeout: timeout,
		params: KDFParams{
			Name: "argon2id", Time: 3, Memory: 64 * 1024, Threads: 2,
		},
	}
}

func (v *Vault) Path() string {
	return v.path
}

func (v *Vault) SetKDFParamsForTest(timeCost, memory uint32, threads uint8) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.params.Time = timeCost
	v.params.Memory = memory
	v.params.Threads = threads
}

func (v *Vault) Create(password string) error {
	if len([]rune(password)) < 8 {
		return errors.New("master password must contain at least 8 characters")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, err := os.Stat(v.path); err == nil {
		return ErrAlreadyExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	salt, err := randomBytes(16)
	if err != nil {
		return err
	}
	params := v.params
	params.Salt = base64.StdEncoding.EncodeToString(salt)
	wrappingKey := deriveKey(password, params, salt)
	defer wipe(wrappingKey)
	dataKey, err := randomBytes(32)
	if err != nil {
		return err
	}
	keyNonce, wrappedKey, err := seal(wrappingKey, dataKey, []byte(keyAAD))
	if err != nil {
		wipe(dataKey)
		return err
	}

	v.env = &envelope{
		Version:    1,
		KDF:        params,
		KeyNonce:   encode(keyNonce),
		WrappedKey: encode(wrappedKey),
	}
	v.dataKey = dataKey
	v.data = &payload{Version: 1, Secrets: []Secret{}, Shortcuts: []Shortcut{}}
	v.lastUsed = time.Now()
	if err := v.persistLocked(); err != nil {
		v.lockLocked()
		return err
	}
	return nil
}

func (v *Vault) Unlock(password string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.lockLocked()
	info, err := os.Stat(v.path)
	if err != nil {
		return fmt.Errorf("read vault metadata: %w", err)
	}
	if info.Size() > maxVaultSize {
		return errors.New("vault file exceeds the 32 MiB safety limit")
	}
	raw, err := os.ReadFile(v.path)
	if err != nil {
		return fmt.Errorf("read vault: %w", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || env.Version != 1 || env.KDF.Name != "argon2id" {
		return ErrInvalidPassword
	}
	if env.KDF.Time < 1 || env.KDF.Time > 10 || env.KDF.Memory < 8*1024 || env.KDF.Memory > 1024*1024 || env.KDF.Threads < 1 || env.KDF.Threads > 16 {
		return ErrInvalidPassword
	}
	salt, err := decode(env.KDF.Salt)
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return ErrInvalidPassword
	}
	wrappingKey := deriveKey(password, env.KDF, salt)
	defer wipe(wrappingKey)
	keyNonce, err := decode(env.KeyNonce)
	if err != nil {
		return ErrInvalidPassword
	}
	wrappedKey, err := decode(env.WrappedKey)
	if err != nil {
		return ErrInvalidPassword
	}
	dataKey, err := open(wrappingKey, keyNonce, wrappedKey, []byte(keyAAD))
	if err != nil {
		return ErrInvalidPassword
	}
	dataNonce, err := decode(env.DataNonce)
	if err != nil {
		wipe(dataKey)
		return ErrInvalidPassword
	}
	ciphertext, err := decode(env.Ciphertext)
	if err != nil {
		wipe(dataKey)
		return ErrInvalidPassword
	}
	plain, err := open(dataKey, dataNonce, ciphertext, []byte(dataAAD))
	if err != nil {
		wipe(dataKey)
		return ErrInvalidPassword
	}
	defer wipe(plain)
	var data payload
	if err := json.Unmarshal(plain, &data); err != nil || data.Version != 1 {
		wipe(dataKey)
		return ErrInvalidPassword
	}
	v.env = &env
	v.data = &data
	v.dataKey = dataKey
	v.lastUsed = time.Now()
	return nil
}

func (v *Vault) Lock() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.lockLocked()
}

func (v *Vault) Status() Status {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.expireLocked()
	_, statErr := os.Stat(v.path)
	status := Status{
		Exists:          statErr == nil,
		Unlocked:        v.data != nil,
		AutoLockSeconds: int(v.timeout.Seconds()),
	}
	if v.data != nil {
		status.StoredSecretCount = len(v.data.Secrets)
		status.StoredShortcutCount = len(v.data.Shortcuts)
		remaining := v.timeout - time.Since(v.lastUsed)
		if remaining > 0 {
			status.RemainingSeconds = int(remaining.Seconds())
		}
	}
	return status
}

var shortcutTrigger = regexp.MustCompile(`^:[a-z0-9][a-z0-9_-]{0,63}$`)

func (v *Vault) ListShortcuts() ([]Shortcut, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlockedLocked(); err != nil {
		return nil, err
	}
	items := append(make([]Shortcut, 0, len(v.data.Shortcuts)), v.data.Shortcuts...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Trigger == items[j].Trigger {
			return items[i].Title < items[j].Title
		}
		return items[i].Trigger < items[j].Trigger
	})
	return items, nil
}

func (v *Vault) FindShortcut(trigger string) (Shortcut, error) {
	trigger = strings.ToLower(strings.TrimSpace(trigger))
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlockedLocked(); err != nil {
		return Shortcut{}, err
	}
	for _, item := range v.data.Shortcuts {
		if strings.EqualFold(item.Trigger, trigger) {
			return item, nil
		}
	}
	return Shortcut{}, ErrShortcutNotFound
}

func (v *Vault) SaveShortcut(item Shortcut) (Shortcut, error) {
	item.Trigger = strings.ToLower(strings.TrimSpace(item.Trigger))
	item.Title = strings.TrimSpace(item.Title)
	item.Category = strings.TrimSpace(item.Category)
	if !shortcutTrigger.MatchString(item.Trigger) {
		return Shortcut{}, errors.New("trigger must start with : and contain only lowercase letters, numbers, _ or -")
	}
	if item.Title == "" {
		item.Title = strings.TrimPrefix(item.Trigger, ":")
	}
	if strings.TrimSpace(item.Content) == "" {
		return Shortcut{}, errors.New("shortcut content is required")
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlockedLocked(); err != nil {
		return Shortcut{}, err
	}
	for _, existing := range v.data.Shortcuts {
		if strings.EqualFold(existing.Trigger, item.Trigger) && existing.ID != item.ID {
			return Shortcut{}, errors.New("shortcut trigger already exists")
		}
	}
	if item.ID == "" {
		idBytes, err := randomBytes(8)
		if err != nil {
			return Shortcut{}, err
		}
		item.ID = fmt.Sprintf("shortcut-%x", idBytes)
	}
	item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	found := false
	for i := range v.data.Shortcuts {
		if v.data.Shortcuts[i].ID == item.ID {
			v.data.Shortcuts[i] = item
			found = true
			break
		}
	}
	if !found {
		v.data.Shortcuts = append(v.data.Shortcuts, item)
	}
	if err := v.persistLocked(); err != nil {
		return Shortcut{}, err
	}
	return item, nil
}

func (v *Vault) DeleteShortcut(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlockedLocked(); err != nil {
		return err
	}
	found := false
	items := v.data.Shortcuts[:0]
	for _, item := range v.data.Shortcuts {
		if item.ID == id {
			found = true
			continue
		}
		items = append(items, item)
	}
	if !found {
		return ErrShortcutNotFound
	}
	v.data.Shortcuts = items
	return v.persistLocked()
}

func (v *Vault) List() ([]SecretMeta, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlockedLocked(); err != nil {
		return nil, err
	}
	items := make([]SecretMeta, len(v.data.Secrets))
	for i, secret := range v.data.Secrets {
		items[i] = SecretMeta{
			ID: secret.ID, Name: secret.Name, Username: secret.Username,
			Notes: secret.Notes, Tags: append([]string(nil), secret.Tags...), UpdatedAt: secret.UpdatedAt,
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (v *Vault) Save(secret Secret) (SecretMeta, error) {
	secret.Name = strings.TrimSpace(secret.Name)
	if secret.Name == "" || secret.Value == "" {
		return SecretMeta{}, errors.New("secret name and value are required")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlockedLocked(); err != nil {
		return SecretMeta{}, err
	}
	if secret.ID == "" {
		idBytes, err := randomBytes(8)
		if err != nil {
			return SecretMeta{}, err
		}
		secret.ID = fmt.Sprintf("secret-%x", idBytes)
	}
	secret.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	found := false
	for i := range v.data.Secrets {
		if v.data.Secrets[i].ID == secret.ID {
			v.data.Secrets[i] = secret
			found = true
			break
		}
	}
	if !found {
		v.data.Secrets = append(v.data.Secrets, secret)
	}
	if err := v.persistLocked(); err != nil {
		return SecretMeta{}, err
	}
	return SecretMeta{ID: secret.ID, Name: secret.Name, Username: secret.Username, Notes: secret.Notes, Tags: secret.Tags, UpdatedAt: secret.UpdatedAt}, nil
}

func (v *Vault) Value(id string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlockedLocked(); err != nil {
		return "", err
	}
	for _, secret := range v.data.Secrets {
		if secret.ID == id {
			return secret.Value, nil
		}
	}
	return "", ErrSecretNotFound
}

func (v *Vault) Delete(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlockedLocked(); err != nil {
		return err
	}
	found := false
	items := v.data.Secrets[:0]
	for _, secret := range v.data.Secrets {
		if secret.ID == id {
			found = true
			continue
		}
		items = append(items, secret)
	}
	if !found {
		return ErrSecretNotFound
	}
	v.data.Secrets = items
	return v.persistLocked()
}

func (v *Vault) requireUnlockedLocked() error {
	v.expireLocked()
	if v.data == nil || len(v.dataKey) == 0 {
		return ErrLocked
	}
	v.lastUsed = time.Now()
	return nil
}

func (v *Vault) expireLocked() {
	if v.data != nil && v.timeout > 0 && time.Since(v.lastUsed) >= v.timeout {
		v.lockLocked()
	}
}

func (v *Vault) lockLocked() {
	if v.data != nil {
		for i := range v.data.Secrets {
			v.data.Secrets[i].Name = ""
			v.data.Secrets[i].Username = ""
			v.data.Secrets[i].Value = ""
			v.data.Secrets[i].Notes = ""
			v.data.Secrets[i].Tags = nil
		}
		for i := range v.data.Shortcuts {
			v.data.Shortcuts[i].Trigger = ""
			v.data.Shortcuts[i].Title = ""
			v.data.Shortcuts[i].Category = ""
			v.data.Shortcuts[i].Kind = ""
			v.data.Shortcuts[i].Template = ""
			v.data.Shortcuts[i].Content = ""
			for variableIndex := range v.data.Shortcuts[i].Variables {
				v.data.Shortcuts[i].Variables[variableIndex].Name = ""
				v.data.Shortcuts[i].Variables[variableIndex].Label = ""
				v.data.Shortcuts[i].Variables[variableIndex].Default = ""
				v.data.Shortcuts[i].Variables[variableIndex].Placeholder = ""
				v.data.Shortcuts[i].Variables[variableIndex].Options = nil
			}
			v.data.Shortcuts[i].Variables = nil
			for key := range v.data.Shortcuts[i].Fields {
				v.data.Shortcuts[i].Fields[key] = ""
				delete(v.data.Shortcuts[i].Fields, key)
			}
			v.data.Shortcuts[i].Fields = nil
		}
	}
	wipe(v.dataKey)
	v.dataKey = nil
	v.data = nil
	v.env = nil
	v.lastUsed = time.Time{}
}

func (v *Vault) persistLocked() error {
	plain, err := json.Marshal(v.data)
	if err != nil {
		return err
	}
	defer wipe(plain)
	nonce, ciphertext, err := seal(v.dataKey, plain, []byte(dataAAD))
	if err != nil {
		return err
	}
	v.env.DataNonce = encode(nonce)
	v.env.Ciphertext = encode(ciphertext)
	v.env.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	raw, err := json.MarshalIndent(v.env, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(v.path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(v.path), ".vault-*.tmp")
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
	return os.Rename(tempName, v.path)
}

func deriveKey(password string, params KDFParams, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, params.Time, params.Memory, params.Threads, 32)
}

func seal(key, plain, aad []byte) ([]byte, []byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, nil, err
	}
	nonce, err := randomBytes(aead.NonceSize())
	if err != nil {
		return nil, nil, err
	}
	return nonce, aead.Seal(nil, nonce, plain, aad), nil
}

func open(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nonce, ciphertext, aad)
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func randomBytes(size int) ([]byte, error) {
	buf := make([]byte, size)
	_, err := rand.Read(buf)
	return buf, err
}

func encode(value []byte) string          { return base64.StdEncoding.EncodeToString(value) }
func decode(value string) ([]byte, error) { return base64.StdEncoding.DecodeString(value) }

func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
