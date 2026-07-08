package redact

import (
	"strings"
	"testing"
)

func TestRedactGoldenCorpus(t *testing.T) {
	engine, err := New(true, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		input    string
		contains string
	}{
		{"email", "Contact me at alice@example.com please", "[EMAIL]"},
		{"email_in_prose", "Send reports to bob.smith+tag@company.co.uk today", "[EMAIL]"},
		{"card_spaced", "Card: 4111 1111 1111 1111", "[CREDIT_CARD]"},
		{"card_dashed", "4111-1111-1111-1111", "[CREDIT_CARD]"},
		{"iban", "Transfer to DE89370400440532013000", "[IBAN]"},
		{"api_key_openai", "key is sk-1234567890abcdefghijklmnopqrst", "[API_KEY]"},
		{"api_key_github", "token ghp_1234567890123456789012345678901234", "[API_KEY]"},
		{"phone_us", "Call +1 (555) 123-4567", "[PHONE]"},
		{"phone_e164", "Reach me at +442071838750", "[PHONE]"},
		{"ipv4", "Server at 192.168.1.1 failed", "[IP]"},
		{"ssn", "SSN 123-45-6789 on file", "[NATIONAL_ID]"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := engine.Redact(tc.input)
			if !strings.Contains(got.Content, tc.contains) {
				t.Fatalf("expected %q in %q (count=%d)", tc.contains, got.Content, got.Count)
			}
			if got.Count < 1 {
				t.Fatalf("expected redaction count >= 1")
			}
		})
	}
}

func TestRedactFalsePositives(t *testing.T) {
	engine, err := New(true, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	cases := []string{
		"Version 1.2.3 released on 2026-07-02",
		"UUID: 550e8400-e29b-41d4-a716-446655440000",
		"Math: 12345 + 67890 = 80235",
		"Code: const x = 4111; // not a card",
		"Date 2026-01-15 is fine",
	}

	for _, input := range cases {
		got := engine.Redact(input)
		if got.Content != input {
			t.Fatalf("false positive: input=%q got=%q count=%d", input, got.Content, got.Count)
		}
	}
}

func TestRedactDisabled(t *testing.T) {
	engine, err := New(false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	input := "email me at secret@example.com"
	got := engine.Redact(input)
	if got.Content != input || got.Count != 0 {
		t.Fatalf("disabled engine should not redact")
	}
}

func FuzzRedactEngine(f *testing.F) {
	engine, err := New(true, nil, "")
	if err != nil {
		f.Fatal(err)
	}
	f.Add("hello world")
	f.Add("email@test.com card 4111111111111111")
	f.Fuzz(func(t *testing.T, input string) {
		got := engine.Redact(input)
		if len(got.Content) > len(input)*2+64 {
			t.Fatalf("output suspiciously longer than input")
		}
	})
}
