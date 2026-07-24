//go:build windows

package hotkey

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const (
	hotkeyID          = 0x4558
	modAlt            = 0x0001
	modControl        = 0x0002
	modNoRepeat       = 0x4000
	vkBack            = 0x08
	vkReturn          = 0x0D
	vkShift           = 0x10
	vkControl         = 0x11
	vkMenu            = 0x12
	vkRight           = 0x27
	vkC               = 0x43
	vkJ               = 0x4A
	wmHotkey          = 0x0312
	wmQuit            = 0x0012
	inputKeyboard     = 1
	keyEventKeyUp     = 0x0002
	keyEventUnicode   = 0x0004
	swRestore         = 9
	processQueryInfo  = 0x1000
	maxKeyboardEvents = 32768
	maxProcessPath    = 32768
)

var (
	user32                 = syscall.NewLazyDLL("user32.dll")
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procRegisterHotKey     = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey   = user32.NewProc("UnregisterHotKey")
	procGetMessage         = user32.NewProc("GetMessageW")
	procPostThreadMessage  = user32.NewProc("PostThreadMessageW")
	procGetForeground      = user32.NewProc("GetForegroundWindow")
	procGetWindowProcessID = user32.NewProc("GetWindowThreadProcessId")
	procGetKeyboardLayout  = user32.NewProc("GetKeyboardLayout")
	procIsWindow           = user32.NewProc("IsWindow")
	procSetForeground      = user32.NewProc("SetForegroundWindow")
	procShowWindow         = user32.NewProc("ShowWindow")
	procKeybdEvent         = user32.NewProc("keybd_event")
	procSendInput          = user32.NewProc("SendInput")
	procVkKeyScanEx        = user32.NewProc("VkKeyScanExW")
	procOpenProcess        = kernel32.NewProc("OpenProcess")
	procQueryProcessName   = kernel32.NewProc("QueryFullProcessImageNameW")
	procCloseHandle        = kernel32.NewProc("CloseHandle")
	procGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
)

type point struct{ X, Y int32 }
type message struct {
	HWnd    uintptr
	Message uint32
	_       uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type implementation struct {
	mu       sync.Mutex
	threadID uint32
	running  bool
}

func (i *implementation) start(callback func(target uintptr)) error {
	i.mu.Lock()
	if i.running {
		i.mu.Unlock()
		return nil
	}
	i.mu.Unlock()
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		thread, _, _ := procGetCurrentThreadID.Call()
		registered, _, callErr := procRegisterHotKey.Call(0, hotkeyID, modAlt|modControl|modNoRepeat, vkJ)
		if registered == 0 {
			ready <- fmt.Errorf("register Ctrl+Alt+J: %w", callErr)
			return
		}
		i.mu.Lock()
		i.threadID = uint32(thread)
		i.running = true
		i.mu.Unlock()
		ready <- nil

		defer func() {
			procUnregisterHotKey.Call(0, hotkeyID)
			i.mu.Lock()
			i.running = false
			i.threadID = 0
			i.mu.Unlock()
		}()
		var msg message
		for {
			result, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(result) <= 0 {
				return
			}
			if msg.Message == wmHotkey && msg.WParam == hotkeyID {
				target, _, _ := procGetForeground.Call()
				callback(target)
			}
		}
	}()
	return <-ready
}

func (i *implementation) stop() {
	i.mu.Lock()
	threadID := i.threadID
	running := i.running
	i.mu.Unlock()
	if running && threadID != 0 {
		procPostThreadMessage.Call(uintptr(threadID), wmQuit, 0, 0)
	}
}

func (i *implementation) insertText(target uintptr, text string, eraseChars int) (string, error) {
	if err := focusTarget(target); err != nil {
		return "", err
	}
	processName, threadID := targetProcess(target)
	mode := "unicode"
	var events []keyboardEvent
	var err error
	if isTerminalProcess(processName) {
		mode = "physical-keyboard"
		layout, _, _ := procGetKeyboardLayout.Call(uintptr(threadID))
		events, err = buildPhysicalTextInputEvents(text, eraseChars, layout)
	} else {
		events, err = buildTextInputEvents(text, eraseChars)
	}
	if err != nil {
		return mode, err
	}
	for _, event := range events {
		if err := sendKeyboardInput(event); err != nil {
			return mode, err
		}
	}
	if processName == "" {
		return mode, nil
	}
	return mode + ":" + processName, nil
}

func (i *implementation) copySelection(target uintptr) error {
	if err := focusTarget(target); err != nil {
		return err
	}
	processName, _ := targetProcess(target)
	if usesTerminalCopyShortcut(processName) {
		sendCtrlShiftKey(vkC)
	} else {
		sendCtrlKey(vkC)
	}
	time.Sleep(80 * time.Millisecond)
	return nil
}

func (i *implementation) foregroundWindow() uintptr {
	target, _, _ := procGetForeground.Call()
	return target
}

func (i *implementation) isExternalTarget(target uintptr) bool {
	if target == 0 {
		return false
	}
	valid, _, _ := procIsWindow.Call(target)
	if valid == 0 {
		return false
	}
	var processID uint32
	procGetWindowProcessID.Call(target, uintptr(unsafe.Pointer(&processID)))
	return processID != 0 && processID != uint32(os.Getpid())
}

func focusTarget(target uintptr) error {
	if target == 0 {
		return errors.New("no previous window is available; content was copied instead")
	}
	procShowWindow.Call(target, swRestore)
	result, _, callErr := procSetForeground.Call(target)
	if result == 0 {
		return fmt.Errorf("focus previous window: %w", callErr)
	}
	time.Sleep(90 * time.Millisecond)
	return nil
}

func sendCtrlKey(key uintptr) {
	procKeybdEvent.Call(vkControl, 0, 0, 0)
	procKeybdEvent.Call(key, 0, 0, 0)
	procKeybdEvent.Call(key, 0, keyEventKeyUp, 0)
	procKeybdEvent.Call(vkControl, 0, keyEventKeyUp, 0)
}

func sendCtrlShiftKey(key uintptr) {
	procKeybdEvent.Call(vkControl, 0, 0, 0)
	procKeybdEvent.Call(vkShift, 0, 0, 0)
	procKeybdEvent.Call(key, 0, 0, 0)
	procKeybdEvent.Call(key, 0, keyEventKeyUp, 0)
	procKeybdEvent.Call(vkShift, 0, keyEventKeyUp, 0)
	procKeybdEvent.Call(vkControl, 0, keyEventKeyUp, 0)
}

type keyboardEvent struct {
	virtualKey uint16
	scanCode   uint16
	flags      uint32
}

func buildTextInputEvents(text string, eraseChars int) ([]keyboardEvent, error) {
	if eraseChars < 0 {
		return nil, errors.New("replacement length cannot be negative")
	}
	runes := []rune(text)
	units := utf16.Encode(runes)
	estimated := eraseChars*2 + len(units)*2
	if eraseChars > 0 {
		estimated += 2
	}
	if estimated > maxKeyboardEvents {
		return nil, fmt.Errorf("expanded text is too large to insert: %d keyboard events", estimated)
	}
	events := make([]keyboardEvent, 0, estimated)
	if eraseChars > 0 {
		events = appendKeyPress(events, vkRight)
		for range eraseChars {
			events = appendKeyPress(events, vkBack)
		}
	}
	for _, unit := range units {
		if unit == '\r' {
			continue
		}
		if unit == '\n' {
			events = appendKeyPress(events, vkReturn)
			continue
		}
		events = append(events,
			keyboardEvent{scanCode: unit, flags: keyEventUnicode},
			keyboardEvent{scanCode: unit, flags: keyEventUnicode | keyEventKeyUp},
		)
	}
	return events, nil
}

func buildPhysicalTextInputEvents(text string, eraseChars int, layout uintptr) ([]keyboardEvent, error) {
	if eraseChars < 0 {
		return nil, errors.New("replacement length cannot be negative")
	}
	runes := []rune(text)
	estimated := eraseChars*2 + len(runes)*8
	if eraseChars > 0 {
		estimated += 2
	}
	if estimated > maxKeyboardEvents {
		return nil, fmt.Errorf("expanded text is too large to insert: up to %d keyboard events", estimated)
	}
	events := make([]keyboardEvent, 0, estimated)
	if eraseChars > 0 {
		events = appendKeyPress(events, vkRight)
		for range eraseChars {
			events = appendKeyPress(events, vkBack)
		}
	}
	for _, character := range runes {
		switch character {
		case '\r':
			continue
		case '\n':
			events = appendKeyPress(events, vkReturn)
			continue
		}
		mapped, _, _ := procVkKeyScanEx.Call(uintptr(character), layout)
		keyAndModifiers := uint16(mapped)
		if keyAndModifiers == 0xffff || byte(keyAndModifiers>>8)&^byte(0x07) != 0 {
			for _, unit := range utf16.Encode([]rune{character}) {
				events = append(events,
					keyboardEvent{scanCode: unit, flags: keyEventUnicode},
					keyboardEvent{scanCode: unit, flags: keyEventUnicode | keyEventKeyUp},
				)
			}
			continue
		}
		key := keyAndModifiers & 0xff
		modifiers := byte(keyAndModifiers >> 8)
		if modifiers&0x02 != 0 {
			events = append(events, keyboardEvent{virtualKey: vkControl})
		}
		if modifiers&0x04 != 0 {
			events = append(events, keyboardEvent{virtualKey: vkMenu})
		}
		if modifiers&0x01 != 0 {
			events = append(events, keyboardEvent{virtualKey: vkShift})
		}
		events = appendKeyPress(events, key)
		if modifiers&0x01 != 0 {
			events = append(events, keyboardEvent{virtualKey: vkShift, flags: keyEventKeyUp})
		}
		if modifiers&0x04 != 0 {
			events = append(events, keyboardEvent{virtualKey: vkMenu, flags: keyEventKeyUp})
		}
		if modifiers&0x02 != 0 {
			events = append(events, keyboardEvent{virtualKey: vkControl, flags: keyEventKeyUp})
		}
	}
	return events, nil
}

func appendKeyPress(events []keyboardEvent, key uint16) []keyboardEvent {
	return append(events,
		keyboardEvent{virtualKey: key},
		keyboardEvent{virtualKey: key, flags: keyEventKeyUp},
	)
}

func sendKeyboardInput(event keyboardEvent) error {
	var raw [40]byte
	binary.LittleEndian.PutUint32(raw[0:4], inputKeyboard)
	unionOffset := 4
	inputSize := uintptr(28)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		unionOffset = 8
		inputSize = 40
	}
	binary.LittleEndian.PutUint16(raw[unionOffset:unionOffset+2], event.virtualKey)
	binary.LittleEndian.PutUint16(raw[unionOffset+2:unionOffset+4], event.scanCode)
	binary.LittleEndian.PutUint32(raw[unionOffset+4:unionOffset+8], event.flags)
	sent, _, callErr := procSendInput.Call(
		1,
		uintptr(unsafe.Pointer(&raw[0])),
		inputSize,
	)
	if sent != 1 {
		if callErr == syscall.Errno(0) {
			return errors.New("Windows rejected simulated input; the target may be running as administrator")
		}
		return fmt.Errorf("send keyboard input: %w", callErr)
	}
	return nil
}

func targetProcess(target uintptr) (string, uint32) {
	var processID uint32
	threadID, _, _ := procGetWindowProcessID.Call(target, uintptr(unsafe.Pointer(&processID)))
	if processID == 0 {
		return "", uint32(threadID)
	}
	handle, _, _ := procOpenProcess.Call(processQueryInfo, 0, uintptr(processID))
	if handle == 0 {
		return "", uint32(threadID)
	}
	defer procCloseHandle.Call(handle)
	buffer := make([]uint16, maxProcessPath)
	size := uint32(len(buffer))
	result, _, _ := procQueryProcessName.Call(
		handle,
		0,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if result == 0 || size == 0 {
		return "", uint32(threadID)
	}
	path := syscall.UTF16ToString(buffer[:size])
	return strings.ToLower(filepath.Base(path)), uint32(threadID)
}

func isTerminalProcess(processName string) bool {
	switch strings.ToLower(processName) {
	case "termius.exe",
		"windowsterminal.exe",
		"xshell.exe",
		"sshell.exe",
		"putty.exe",
		"mobaxterm.exe",
		"securecrt.exe",
		"wezterm-gui.exe",
		"alacritty.exe",
		"kitty.exe",
		"tabby.exe",
		"electerm.exe",
		"mintty.exe",
		"conhost.exe":
		return true
	default:
		return false
	}
}

func usesTerminalCopyShortcut(processName string) bool {
	switch strings.ToLower(processName) {
	case "termius.exe", "windowsterminal.exe", "tabby.exe", "electerm.exe":
		return true
	default:
		return false
	}
}
