package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestWorkerErrorHint(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"wrap timeout", fmt.Errorf("WRAP_AUTH_TIMEOUT: DTLS timeout"), "WRAP/DTLS"},
		{"quota", fmt.Errorf("%w: Allocate error response (error 486)", ErrTurnQuota), "TURN-слоты"},
		{"rate limit", errors.New("VK error 29: rate limit"), "ограничил запросы"},
		{"bad password", errors.New("FATAL_AUTH: неверный пароль"), "неверный пароль"},
		{"unknown", errors.New("something strange"), ""},
		{"nil", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := workerErrorHint(tc.err)
			if tc.want == "" {
				if got != "" {
					t.Fatalf("workerErrorHint() = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("workerErrorHint() = %q, want substring %q", got, tc.want)
			}
		})
	}
}

// TestQuotaCooldown — cooldown выставляется, не сокращается повторным
// вызовом и истекает.
func TestQuotaCooldown(t *testing.T) {
	var cache StreamCredentialsCache
	if got := cache.quotaRemaining(); got != 0 {
		t.Fatalf("quotaRemaining() = %v, want 0", got)
	}

	cache.setQuotaCooldown(80 * time.Millisecond)
	if got := cache.quotaRemaining(); got <= 0 || got > 80*time.Millisecond {
		t.Fatalf("quotaRemaining() = %v, want (0; 80ms]", got)
	}

	// Повторный (меньший) cooldown не должен сокращать текущий.
	cache.setQuotaCooldown(time.Millisecond)
	if got := cache.quotaRemaining(); got < 40*time.Millisecond {
		t.Fatalf("cooldown сокращён: %v", got)
	}

	time.Sleep(90 * time.Millisecond)
	if got := cache.quotaRemaining(); got != 0 {
		t.Fatalf("cooldown не истёк: %v", got)
	}
}
