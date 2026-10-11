package main

import "testing"

func TestRunRequiresCredentials(t *testing.T) {
	if err := run([]string{"-login", "", "-pass", ""}); err == nil {
		t.Fatal("ожидалась ошибка без логина и пароля")
	}
}

func TestRunBadOrigin(t *testing.T) {
	if err := run([]string{"-login", "a", "-pass", "b", "-origin", "x"}); err == nil {
		t.Fatal("ожидалась ошибка origin")
	}
}
