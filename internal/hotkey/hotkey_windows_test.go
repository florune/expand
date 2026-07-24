//go:build windows

package hotkey

import "testing"

func TestBuildTextInputEventsUsesUnicodeAndPhysicalNewline(t *testing.T) {
	events, err := buildTextInputEvents("A🙂\r\nB", 0)
	if err != nil {
		t.Fatal(err)
	}
	// A, two UTF-16 surrogate units, Enter and B; every input has key down/up.
	if len(events) != 10 {
		t.Fatalf("expected 10 events, got %d", len(events))
	}
	if events[0].scanCode != 'A' || events[0].flags != keyEventUnicode {
		t.Fatalf("first event is not Unicode A: %+v", events[0])
	}
	if events[6].virtualKey != vkReturn || events[6].flags != 0 {
		t.Fatalf("newline was not converted to Enter: %+v", events[6])
	}
}

func TestBuildTextInputEventsErasesSelectedTrigger(t *testing.T) {
	events, err := buildTextInputEvents("ok", len([]rune(":mysql-connect")))
	if err != nil {
		t.Fatal(err)
	}
	if events[0].virtualKey != vkRight || events[1].virtualKey != vkRight {
		t.Fatal("replacement must first collapse the selection at its right edge")
	}
	if events[2].virtualKey != vkBack || events[2].flags != 0 {
		t.Fatal("replacement must erase the trigger before inserting text")
	}
}

func TestBuildTextInputEventsRejectsOversizedInput(t *testing.T) {
	text := make([]rune, maxKeyboardEvents)
	for index := range text {
		text[index] = 'x'
	}
	if _, err := buildTextInputEvents(string(text), 0); err == nil {
		t.Fatal("expected oversized input to be rejected")
	}
}

func TestBuildPhysicalTextInputEventsUsesVirtualKeysForASCII(t *testing.T) {
	layout, _, _ := procGetKeyboardLayout.Call(0)
	events, err := buildPhysicalTextInputEvents("abc", 0, layout)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 6 {
		t.Fatalf("expected six key events, got %d", len(events))
	}
	for _, event := range events {
		if event.flags&keyEventUnicode != 0 {
			t.Fatalf("ASCII terminal input unexpectedly used Unicode packet: %+v", event)
		}
		if event.virtualKey == 0 {
			t.Fatalf("ASCII terminal input has no virtual key: %+v", event)
		}
	}
}

func TestTerminalProcessDetection(t *testing.T) {
	for _, name := range []string{"Termius.exe", "WindowsTerminal.exe", "Xshell.exe", "putty.exe"} {
		if !isTerminalProcess(name) {
			t.Fatalf("%s was not detected as a terminal", name)
		}
	}
	if isTerminalProcess("chrome.exe") {
		t.Fatal("regular Chromium windows must keep Unicode input mode")
	}
	if !usesTerminalCopyShortcut("Termius.exe") {
		t.Fatal("Termius must use Ctrl+Shift+C to copy its selected terminal text")
	}
}
