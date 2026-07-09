package router_test

import (
	"testing"

	"github.com/yadra-team/yadra-bridge/internal/router"
)

func TestTierAllows(t *testing.T) {
	cases := []struct {
		user, min string
		want      bool
	}{
		{"pro", "free", true},
		{"free", "pro", false},
		{"power", "pro", true},
		{"trial", "trial", true},
	}
	for _, c := range cases {
		if got := router.TierAllows(c.user, c.min); got != c.want {
			t.Fatalf("TierAllows(%q,%q)=%v want %v", c.user, c.min, got, c.want)
		}
	}
}
