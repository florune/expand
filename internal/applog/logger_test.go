package applog

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoggerRedactsCredentials(t *testing.T) {
	var output bytes.Buffer
	logger := &Logger{out: &output}
	logger.Frontend("error", "password=hunter2 token:abc123", "mysql://root:secret@db.internal/app")
	text := output.String()
	for _, secret := range []string{"hunter2", "abc123", "root:secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("log contains secret %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("redaction marker missing: %s", text)
	}
}
