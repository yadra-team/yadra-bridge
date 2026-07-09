package logging_test

import (
	"bytes"
	"testing"

	"github.com/yadra-team/yadra-bridge/internal/logging"
)

func TestLoggerMetadataOnlyShape(t *testing.T) {
	var buf bytes.Buffer
	log := logging.New().Output(&buf)
	log.Info().
		Str("model", "gpt-4o-mini").
		Str("user_sub", "user-123").
		Str("tier", "pro").
		Msg("routing request")

	out := buf.String()
	if out == "" {
		t.Fatal("expected log output")
	}
	for _, forbidden := range []string{"messages", "prompt", "content", "response"} {
		if containsField(out, forbidden) {
			t.Fatalf("log must not include %q field", forbidden)
		}
	}
}

func containsField(line, field string) bool {
	return bytes.Contains([]byte(line), []byte(`"`+field+`"`))
}
