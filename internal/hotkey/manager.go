package hotkey

import (
	"sync"
	"time"
)

type Manager struct {
	mu          sync.RWMutex
	target      uintptr
	impl        implementation
	watcherStop chan struct{}
	watcherDone chan struct{}
}

func New() *Manager { return &Manager{} }

func (m *Manager) Start(callback func(target uintptr)) error {
	err := m.impl.start(func(target uintptr) {
		m.SetTarget(target)
		callback(target)
	})
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.watcherStop == nil {
		m.watcherStop = make(chan struct{})
		m.watcherDone = make(chan struct{})
		go m.watchForeground(m.watcherStop, m.watcherDone)
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) Stop() {
	m.mu.Lock()
	stop := m.watcherStop
	done := m.watcherDone
	m.watcherStop = nil
	m.watcherDone = nil
	m.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
	m.impl.stop()
}

func (m *Manager) SetTarget(target uintptr) {
	if !m.impl.isExternalTarget(target) {
		return
	}
	m.mu.Lock()
	m.target = target
	m.mu.Unlock()
}

func (m *Manager) HasTarget() bool {
	m.mu.RLock()
	target := m.target
	m.mu.RUnlock()
	return m.impl.isExternalTarget(target)
}

func (m *Manager) InsertText(text string) (string, error) {
	m.mu.RLock()
	target := m.target
	m.mu.RUnlock()
	return m.impl.insertText(target, text, 0)
}

func (m *Manager) ReplaceSelection(selected, text string) (string, error) {
	m.mu.RLock()
	target := m.target
	m.mu.RUnlock()
	return m.impl.insertText(target, text, len([]rune(selected)))
}

func (m *Manager) CopySelection() error {
	m.mu.RLock()
	target := m.target
	m.mu.RUnlock()
	return m.impl.copySelection(target)
}

func (m *Manager) watchForeground(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.SetTarget(m.impl.foregroundWindow())
		case <-stop:
			return
		}
	}
}
