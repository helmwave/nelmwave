package log

import "testing"

func TestNew_Formats(t *testing.T) {
	for _, format := range []Format{FormatAuto, FormatConsole, FormatJSON} {
		logger, err := New(Options{Level: "info", Format: format})
		if err != nil {
			t.Errorf("New(%q) failed: %v", format, err)
			continue
		}
		if logger == nil {
			t.Errorf("New(%q) returned nil logger", format)
		}
	}
}

func TestNew_RejectsBadLevel(t *testing.T) {
	if _, err := New(Options{Level: "chatty", Format: FormatJSON}); err == nil {
		t.Error("expected error for invalid level, got nil")
	}
}

func TestNew_RejectsBadFormat(t *testing.T) {
	if _, err := New(Options{Level: "info", Format: "yaml"}); err == nil {
		t.Error("expected error for invalid format, got nil")
	}
}
