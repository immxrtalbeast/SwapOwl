package main

import "testing"

func TestRunRequiresEnv(t *testing.T) {
	for _, n := range []string{"API_URL", "API_KEY", "TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID"} {
		t.Setenv(n, "")
	}
	if err := run(); err == nil {
		t.Fatal("ожидалась ошибка без переменных окружения")
	}
}

func TestRunBadDigestTime(t *testing.T) {
	for _, n := range []string{"API_URL", "API_KEY", "TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID"} {
		t.Setenv(n, "x")
	}
	t.Setenv("NOTIFY_DIGEST_AT", "25:99")
	if err := run(); err == nil {
		t.Fatal("ожидалась ошибка формата")
	}
}
