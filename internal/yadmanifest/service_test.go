package yadmanifest

import "testing"

func TestValidateManifest(t *testing.T) {
	valid := Manifest{
		Models: []Entry{{
			Version:       "y0.5-base",
			URL:           "https://example.com/model.gguf",
			SHA256:        "6a1a2eb6d15622bf3c96857206351ba97e1af16c30d7a74ee38970e434e9407e",
			SizeBytes:     100,
			MinAppVersion: "0.1.0",
		}},
	}
	if err := validateManifest(valid); err != nil {
		t.Fatalf("expected valid manifest: %v", err)
	}

	cases := []Manifest{
		{Models: nil},
		{Models: []Entry{{Version: "", URL: "https://x", MinAppVersion: "0.1.0"}}},
		{Models: []Entry{{Version: "v", URL: "", MinAppVersion: "0.1.0"}}},
		{Models: []Entry{{Version: "v", URL: "https://x", MinAppVersion: ""}}},
		{Models: []Entry{{Version: "v", URL: "https://x", MinAppVersion: "0.1.0", SHA256: "bad"}}},
	}
	for i, m := range cases {
		if err := validateManifest(m); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}
