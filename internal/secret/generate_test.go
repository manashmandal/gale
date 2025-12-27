package secret

import (
	"testing"
)

func TestGenerateWebhookSecret(t *testing.T) {
	s1, err := GenerateWebhookSecret()
	if err != nil {
		t.Fatalf("GenerateWebhookSecret() error = %v", err)
	}

	if len(s1) != 64 {
		t.Errorf("GenerateWebhookSecret() length = %d, want 64", len(s1))
	}

	s2, err := GenerateWebhookSecret()
	if err != nil {
		t.Fatalf("GenerateWebhookSecret() error = %v", err)
	}

	if s1 == s2 {
		t.Error("GenerateWebhookSecret() generated same secret twice, should be unique")
	}
}

func TestGenerateWebhookSecret_IsHex(t *testing.T) {
	s, err := GenerateWebhookSecret()
	if err != nil {
		t.Fatalf("GenerateWebhookSecret() error = %v", err)
	}

	for i, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("GenerateWebhookSecret() char at %d = %c, want hex character", i, c)
		}
	}
}
