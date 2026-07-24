package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/florune/expand/internal/applog"
	"github.com/florune/expand/internal/hotkey"
	"github.com/florune/expand/internal/library"
	"github.com/florune/expand/internal/model"
	"github.com/florune/expand/internal/profile"
	templater "github.com/florune/expand/internal/template"
	"github.com/florune/expand/internal/vault"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	defaultClipboardTTL = 20 * time.Second
	defaultVaultTTL     = 24 * time.Hour
)

var selectedTriggerPattern = regexp.MustCompile(`^:[a-z0-9][a-z0-9_-]{0,63}$`)
var connectionFieldPattern = regexp.MustCompile(`^[a-zA-Z0-9._:$-]+$`)

type BootstrapState struct {
	Entries         []model.Entry      `json:"entries"`
	Categories      []string           `json:"categories"`
	Profiles        []profile.Profile  `json:"profiles"`
	ActiveProfile   *profile.Profile   `json:"activeProfile,omitempty"`
	Vault           vault.Status       `json:"vault"`
	Shortcuts       []vault.Shortcut   `json:"shortcuts"`
	Secrets         []vault.SecretMeta `json:"secrets"`
	LibraryDir      string             `json:"libraryDir"`
	ProfileRoot     string             `json:"profileRoot"`
	VaultFile       string             `json:"vaultFile,omitempty"`
	HotkeyAvailable bool               `json:"hotkeyAvailable"`
	HotkeyMessage   string             `json:"hotkeyMessage"`
	LogFile         string             `json:"logFile"`
	InitError       string             `json:"initError,omitempty"`
}

type App struct {
	ctx         context.Context
	store       *library.Store
	profiles    *profile.Manager
	active      *profile.Profile
	vault       *vault.Vault
	hotkey      *hotkey.Manager
	libraryDir  string
	profileRoot string
	initErr     error
	log         *applog.Logger

	mu              sync.Mutex
	hotkeyAvailable bool
	hotkeyMessage   string
	clipboardSerial uint64
}

func NewApp() *App {
	logger, logErr := applog.New()
	if logErr != nil {
		logger = applog.Discard()
		fmt.Printf("initialise expand log: %v\n", logErr)
	}
	libraryDir, profileRoot, err := resolvePaths()
	application := &App{
		libraryDir:  libraryDir,
		profileRoot: profileRoot,
		store:       library.New(libraryDir),
		profiles:    profile.New(profileRoot),
		hotkey:      hotkey.New(),
		initErr:     err,
		log:         logger,
	}
	application.log.Info("app.initialise", "starting expand")
	if err == nil {
		application.initErr = syncBuiltinTemplates(libraryDir)
	}
	if application.initErr == nil {
		application.initErr = application.store.Load()
	}
	if application.initErr != nil {
		application.log.Error("app.initialise", application.initErr)
	} else {
		application.log.Info("app.initialise", "paths and built-in library loaded")
	}
	return application
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.log.Info("app.startup", "Wails runtime started")
	if a.initErr != nil {
		runtime.LogErrorf(ctx, "initialise expand: %v", a.initErr)
	}
	err := a.hotkey.Start(func(uintptr) {
		go a.handleGlobalHotkey()
	})
	a.mu.Lock()
	if err != nil {
		a.hotkeyAvailable = false
		a.hotkeyMessage = err.Error()
		a.log.Error("hotkey.register", err)
	} else {
		a.hotkeyAvailable = true
		a.hotkeyMessage = "Ctrl + Alt + J"
		a.log.Info("hotkey.register", "global shortcut registered")
	}
	a.mu.Unlock()
	a.setCompactWindow()
}

func (a *App) shutdown(context.Context) {
	a.log.Info("app.shutdown", "shutting down")
	a.hotkey.Stop()
	if current := a.activeVault(); current != nil {
		current.Lock()
	}
	_ = a.log.Close()
}

func (a *App) beforeClose(context.Context) bool {
	a.log.Info("app.close", "window close requested; exiting process")
	return false
}

func (a *App) Quit() {
	runtime.Quit(a.ctx)
}

func (a *App) Bootstrap() BootstrapState {
	state := BootstrapState{
		Entries:     a.store.List(),
		LibraryDir:  a.libraryDir,
		ProfileRoot: a.profileRoot,
		LogFile:     a.log.Path(),
		Shortcuts:   []vault.Shortcut{},
		Secrets:     []vault.SecretMeta{},
	}
	state.Categories = categories(state.Entries)
	state.Profiles, _ = a.profiles.List()

	a.mu.Lock()
	if a.active != nil {
		item := *a.active
		state.ActiveProfile = &item
	}
	current := a.vault
	state.HotkeyAvailable = a.hotkeyAvailable
	state.HotkeyMessage = a.hotkeyMessage
	a.mu.Unlock()

	if current != nil {
		state.Vault = current.Status()
		state.VaultFile = current.Path()
		if state.Vault.Unlocked {
			state.Shortcuts, _ = current.ListShortcuts()
			state.Secrets, _ = current.List()
		}
	}
	if a.initErr != nil {
		state.InitError = a.initErr.Error()
	}
	return state
}

func (a *App) CreateProfile(name, password string) (BootstrapState, error) {
	if len([]rune(password)) < 8 {
		return BootstrapState{}, errors.New("master password must contain at least 8 characters")
	}
	item, err := a.profiles.Create(name)
	if err != nil {
		a.log.Error("profile.create", err)
		return BootstrapState{}, err
	}
	current := vault.New(a.profiles.VaultPath(item.ID), defaultVaultTTL)
	if err := current.Create(password); err != nil {
		a.log.Error("profile.create_vault", err)
		return BootstrapState{}, err
	}
	a.activateProfile(item, current)
	a.log.Info("profile.create", "local profile created and unlocked")
	runtime.EventsEmit(a.ctx, "expand:session-changed")
	return a.Bootstrap(), nil
}

func (a *App) UnlockProfile(id, password string) (BootstrapState, error) {
	item, err := a.profiles.Get(id)
	if err != nil {
		a.log.Error("profile.unlock", err)
		return BootstrapState{}, err
	}
	current := vault.New(a.profiles.VaultPath(item.ID), defaultVaultTTL)
	if err := current.Unlock(password); err != nil {
		a.log.Error("profile.unlock", err)
		return BootstrapState{}, err
	}
	item, err = a.profiles.Touch(item.ID)
	if err != nil {
		current.Lock()
		return BootstrapState{}, err
	}
	a.activateProfile(item, current)
	a.log.Info("profile.unlock", "local profile unlocked")
	runtime.EventsEmit(a.ctx, "expand:session-changed")
	return a.Bootstrap(), nil
}

func (a *App) LockProfile() BootstrapState {
	if current := a.activeVault(); current != nil {
		current.Lock()
	}
	a.log.Info("profile.lock", "active profile locked")
	runtime.EventsEmit(a.ctx, "expand:session-changed")
	return a.Bootstrap()
}

func (a *App) SwitchProfile() BootstrapState {
	a.mu.Lock()
	if a.vault != nil {
		a.vault.Lock()
	}
	a.vault = nil
	a.active = nil
	a.mu.Unlock()
	a.log.Info("profile.switch", "active profile cleared")
	runtime.EventsEmit(a.ctx, "expand:session-changed")
	return a.Bootstrap()
}

func (a *App) activateProfile(item profile.Profile, current *vault.Vault) {
	a.mu.Lock()
	if a.vault != nil {
		a.vault.Lock()
	}
	a.active = &item
	a.vault = current
	a.mu.Unlock()
}

func (a *App) ReloadLibrary() (BootstrapState, error) {
	if err := a.store.Load(); err != nil {
		return BootstrapState{}, err
	}
	a.initErr = nil
	return a.Bootstrap(), nil
}

func (a *App) RenderEntry(id string, values map[string]string) (string, error) {
	entry, err := a.store.Get(id)
	if err != nil {
		return "", err
	}
	return templater.Render(entry, values)
}

func (a *App) SaveShortcut(item vault.Shortcut, password string) (vault.Shortcut, error) {
	current, err := a.requireActiveVault()
	if err != nil {
		return vault.Shortcut{}, err
	}
	item, err = normaliseShortcut(item)
	if err != nil {
		return vault.Shortcut{}, err
	}
	if password != "" {
		secret, secretErr := current.Save(vault.Secret{
			ID: item.SecretID, Name: item.Title + " 密码", Value: password,
			Tags: []string{"shortcut", strings.TrimPrefix(item.Trigger, ":")},
		})
		if secretErr != nil {
			return vault.Shortcut{}, secretErr
		}
		item.SecretID = secret.ID
	}
	saved, err := current.SaveShortcut(item)
	if err == nil {
		a.log.Info("shortcut.save", "encrypted shortcut persisted")
		runtime.EventsEmit(a.ctx, "expand:shortcuts-changed")
	} else {
		a.log.Error("shortcut.save", err)
	}
	return saved, err
}

func normaliseShortcut(item vault.Shortcut) (vault.Shortcut, error) {
	item.Kind = strings.ToLower(strings.TrimSpace(item.Kind))
	if item.Kind == "mysql" && strings.TrimSpace(item.Template) == "" {
		legacy, err := migrateLegacyMySQLShortcut(item)
		if err != nil {
			return vault.Shortcut{}, err
		}
		item = legacy
	} else if item.Kind != "" && item.Kind != "text" {
		return vault.Shortcut{}, errors.New("unsupported legacy shortcut kind")
	}
	item.Kind = ""
	if strings.TrimSpace(item.Category) == "" {
		item.Category = "common"
	}
	if strings.TrimSpace(item.Template) == "" {
		item.Template = item.Content
	}
	if strings.TrimSpace(item.Template) == "" {
		return vault.Shortcut{}, errors.New("shortcut template is required")
	}
	item.Variables = templater.Variables(item.Template, item.Variables)
	if item.Fields == nil {
		item.Fields = make(map[string]string)
	}
	values := make(map[string]string, len(item.Variables))
	for _, variable := range item.Variables {
		value := item.Fields[variable.Name]
		if strings.TrimSpace(value) == "" {
			value = variable.Default
		}
		values[variable.Name] = value
	}
	item.Fields = values
	content, err := renderShortcut(item)
	if err != nil {
		return vault.Shortcut{}, err
	}
	item.Content = content
	return item, nil
}

func migrateLegacyMySQLShortcut(item vault.Shortcut) (vault.Shortcut, error) {
	if item.Fields == nil {
		return vault.Shortcut{}, errors.New("MySQL connection fields are required")
	}
	host := strings.TrimSpace(item.Fields["host"])
	port := strings.TrimSpace(item.Fields["port"])
	username := strings.TrimSpace(item.Fields["username"])
	database := strings.TrimSpace(item.Fields["database"])
	if port == "" {
		port = "3306"
	}
	for label, value := range map[string]string{
		"host": host, "port": port, "username": username, "database": database,
	} {
		if value == "" || !connectionFieldPattern.MatchString(value) {
			return vault.Shortcut{}, fmt.Errorf("%s contains unsupported characters", label)
		}
	}
	item.Template = "mysql --host={{MYSQL_HOST}} --port={{MYSQL_PORT}} --user={{MYSQL_USER}} -p {{MYSQL_DATABASE}}"
	item.Variables = []model.Variable{
		{Name: "MYSQL_HOST", Label: "主机", Type: "text", Default: "MYSQL_HOST", Required: true},
		{Name: "MYSQL_PORT", Label: "端口", Type: "text", Default: "3306"},
		{Name: "MYSQL_USER", Label: "用户名", Type: "text", Default: "MYSQL_USER", Required: true},
		{Name: "MYSQL_DATABASE", Label: "数据库", Type: "text", Default: "MYSQL_DATABASE", Required: true},
	}
	item.Fields = map[string]string{
		"MYSQL_HOST": host, "MYSQL_PORT": port, "MYSQL_USER": username, "MYSQL_DATABASE": database,
	}
	item.Category = "mysql"
	return item, nil
}

func renderShortcut(item vault.Shortcut) (string, error) {
	if strings.TrimSpace(item.Template) == "" {
		return item.Content, nil
	}
	return templater.Render(model.Entry{
		Template:  item.Template,
		Variables: item.Variables,
	}, item.Fields)
}

func (a *App) DeleteShortcut(id string) error {
	current, err := a.requireActiveVault()
	if err != nil {
		return err
	}
	if err := current.DeleteShortcut(id); err != nil {
		a.log.Error("shortcut.delete", err)
		return err
	}
	a.log.Info("shortcut.delete", "encrypted shortcut deleted")
	runtime.EventsEmit(a.ctx, "expand:shortcuts-changed")
	return nil
}

func (a *App) ListShortcuts() ([]vault.Shortcut, error) {
	current, err := a.requireActiveVault()
	if err != nil {
		return nil, err
	}
	return current.ListShortcuts()
}

func (a *App) UseShortcut(id string) (string, error) {
	content, err := a.RenderShortcut(id)
	if err != nil {
		return "", err
	}
	return a.InsertText(content)
}

func (a *App) RenderShortcut(id string) (string, error) {
	current, err := a.requireActiveVault()
	if err != nil {
		return "", err
	}
	items, err := current.ListShortcuts()
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.ID == id {
			return renderShortcut(item)
		}
	}
	return "", vault.ErrShortcutNotFound
}

func (a *App) CopyText(text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("nothing to copy")
	}
	runtime.ClipboardSetText(a.ctx, text)
	return nil
}

func (a *App) InsertText(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", errors.New("nothing to insert")
	}
	if !a.hotkey.HasTarget() {
		if err := a.CopyText(text); err != nil {
			return "", err
		}
		a.log.Info("hotkey.insert", "no external target; text copied instead")
		return "copied", nil
	}
	runtime.WindowHide(a.ctx)
	mode, err := a.hotkey.InsertText(text)
	if err != nil {
		a.log.Error("hotkey.insert", err)
		a.showCompact("search", "")
		return "", err
	}
	a.log.Info("hotkey.insert", "text inserted using "+mode)
	return mode, nil
}

func (a *App) OpenManager() {
	runtime.WindowSetSize(a.ctx, 1040, 720)
	runtime.WindowCenter(a.ctx)
	runtime.WindowShow(a.ctx)
	runtime.EventsEmit(a.ctx, "expand:open-manager")
}

func (a *App) OpenCompact() {
	a.showCompact("search", "")
}

func (a *App) FrontendReady() {
	a.log.Info("frontend.ready", "Vue application mounted")
}

func (a *App) FrontendLog(level, message, stack string) {
	a.log.Frontend(level, message, stack)
}

func (a *App) HideWindow() {
	runtime.WindowHide(a.ctx)
}

func (a *App) ListSecrets() ([]vault.SecretMeta, error) {
	current, err := a.requireActiveVault()
	if err != nil {
		return nil, err
	}
	return current.List()
}

func (a *App) SaveSecret(secret vault.Secret) (vault.SecretMeta, error) {
	current, err := a.requireActiveVault()
	if err != nil {
		return vault.SecretMeta{}, err
	}
	meta, err := current.Save(secret)
	if err == nil {
		runtime.EventsEmit(a.ctx, "expand:vault-changed")
	}
	return meta, err
}

func (a *App) DeleteSecret(id string) error {
	current, err := a.requireActiveVault()
	if err != nil {
		return err
	}
	if err := current.Delete(id); err != nil {
		return err
	}
	runtime.EventsEmit(a.ctx, "expand:vault-changed")
	return nil
}

func (a *App) CopySecret(id string) error {
	current, err := a.requireActiveVault()
	if err != nil {
		return err
	}
	value, err := current.Value(id)
	if err != nil {
		return err
	}
	runtime.ClipboardSetText(a.ctx, value)
	a.mu.Lock()
	a.clipboardSerial++
	serial := a.clipboardSerial
	a.mu.Unlock()
	go a.clearClipboardAfter(value, serial, defaultClipboardTTL)
	return nil
}

func (a *App) handleGlobalHotkey() {
	if a.ctx == nil {
		return
	}
	original, _ := runtime.ClipboardGetText(a.ctx)
	sentinel := fmt.Sprintf("expand-selection-%d", time.Now().UnixNano())
	runtime.ClipboardSetText(a.ctx, sentinel)
	if err := a.hotkey.CopySelection(); err != nil {
		a.log.Error("hotkey.copy_selection", err)
		runtime.ClipboardSetText(a.ctx, original)
		a.showCompact("search", "")
		return
	}
	selected, _ := runtime.ClipboardGetText(a.ctx)
	runtime.ClipboardSetText(a.ctx, original)
	selected = strings.ToLower(strings.TrimSpace(selected))
	if !selectedTriggerPattern.MatchString(selected) {
		a.showCompact("search", "")
		return
	}

	if current := a.activeVault(); current != nil && current.Status().Unlocked {
		if item, err := current.FindShortcut(selected); err == nil {
			if output, renderErr := renderShortcut(item); renderErr == nil {
				a.log.Info("hotkey.replace", "encrypted shortcut matched; replacing selection")
				a.replaceSelection(selected, output)
				return
			} else {
				a.log.Error("shortcut.render", renderErr)
			}
		}
	}
	for _, entry := range a.store.List() {
		if strings.EqualFold(entry.Trigger, selected) {
			output, err := templater.Render(entry, nil)
			if err == nil {
				a.log.Info("hotkey.replace", "built-in shortcut matched; replacing selection")
				a.replaceSelection(selected, output)
				return
			}
			a.showCompact("create", selected)
			a.log.Info("hotkey.fallback", "matched template requires configuration")
			return
		}
	}
	a.showCompact("create", selected)
	a.log.Info("hotkey.fallback", "selected trigger was not found")
}

func (a *App) replaceSelection(selected, text string) {
	if strings.TrimSpace(text) == "" {
		a.showCompact("create", "")
		return
	}
	mode, err := a.hotkey.ReplaceSelection(selected, text)
	if err != nil {
		a.log.Error("hotkey.insert", err)
		a.showCompact("search", "")
		return
	}
	a.log.Info("hotkey.insert", "selection replaced using "+mode)
}

func (a *App) clearClipboardAfter(secret string, serial uint64, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	<-timer.C
	a.mu.Lock()
	currentSerial := a.clipboardSerial
	a.mu.Unlock()
	if serial != currentSerial || a.ctx == nil {
		return
	}
	if current, err := runtime.ClipboardGetText(a.ctx); err == nil && current == secret {
		runtime.ClipboardSetText(a.ctx, "")
		runtime.EventsEmit(a.ctx, "expand:clipboard-cleared")
	}
}

func (a *App) showCompact(mode, trigger string) {
	a.setCompactWindow()
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowCenter(a.ctx)
	runtime.EventsEmit(a.ctx, "expand:quick-action", map[string]string{
		"mode": mode, "trigger": trigger,
	})
}

func (a *App) setCompactWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowSetSize(a.ctx, 520, 620)
	runtime.WindowCenter(a.ctx)
}

func (a *App) activeVault() *vault.Vault {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.vault
}

func (a *App) requireActiveVault() (*vault.Vault, error) {
	current := a.activeVault()
	if current == nil {
		return nil, errors.New("select and unlock a local profile first")
	}
	if !current.Status().Unlocked {
		return nil, vault.ErrLocked
	}
	return current, nil
}

func categories(entries []model.Entry) []string {
	set := make(map[string]struct{})
	for _, entry := range entries {
		set[entry.Category] = struct{}{}
	}
	items := make([]string, 0, len(set))
	for category := range set {
		items = append(items, category)
	}
	sort.Strings(items)
	return items
}

func resolvePaths() (string, string, error) {
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve user config directory: %w", err)
	}
	appDir := filepath.Join(configRoot, "expand")
	libraryDir := strings.TrimSpace(os.Getenv("EXPAND_LIBRARY_DIR"))
	if libraryDir == "" {
		libraryDir = filepath.Join(appDir, "library")
	}
	profileRoot := strings.TrimSpace(os.Getenv("EXPAND_PROFILE_ROOT"))
	if profileRoot == "" {
		profileRoot = appDir
	}
	if err := os.MkdirAll(libraryDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create library directory: %w", err)
	}
	if err := os.MkdirAll(profileRoot, 0o700); err != nil {
		return "", "", fmt.Errorf("create profile directory: %w", err)
	}
	return libraryDir, profileRoot, nil
}

func syncBuiltinTemplates(libraryDir string) error {
	sourceEntries, err := builtinTemplates.ReadDir("data")
	if err != nil {
		return fmt.Errorf("read embedded templates: %w", err)
	}
	targetDir := filepath.Join(libraryDir, "_builtin")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return fmt.Errorf("create built-in template directory: %w", err)
	}
	current := make(map[string]struct{})
	for _, entry := range sourceEntries {
		if entry.IsDir() {
			continue
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension != ".yml" && extension != ".yaml" {
			continue
		}
		raw, err := builtinTemplates.ReadFile(filepath.ToSlash(filepath.Join("data", entry.Name())))
		if err != nil {
			return fmt.Errorf("read embedded template %s: %w", entry.Name(), err)
		}
		target := filepath.Join(targetDir, entry.Name())
		if err := os.WriteFile(target, raw, 0o600); err != nil {
			return fmt.Errorf("write built-in template %s: %w", entry.Name(), err)
		}
		current[strings.ToLower(entry.Name())] = struct{}{}
	}
	existing, err := os.ReadDir(targetDir)
	if err != nil {
		return fmt.Errorf("scan built-in template directory: %w", err)
	}
	for _, entry := range existing {
		if entry.IsDir() {
			continue
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension != ".yml" && extension != ".yaml" {
			continue
		}
		if _, ok := current[strings.ToLower(entry.Name())]; ok {
			continue
		}
		if err := os.Remove(filepath.Join(targetDir, entry.Name())); err != nil {
			return fmt.Errorf("remove obsolete built-in template %s: %w", entry.Name(), err)
		}
	}
	return nil
}
