package repl

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	cleanup, err := isolateProcessEnv("tanya-repl")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		os.Exit(1)
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func isolateProcessEnv(prefix string) (func(), error) {
	home, err := os.MkdirTemp("", prefix+"-home-")
	if err != nil {
		return nil, err
	}
	wd, err := os.MkdirTemp("", prefix+"-wd-")
	if err != nil {
		os.RemoveAll(home)
		return nil, err
	}
	old, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err := os.Setenv("HOME", home); err != nil {
		return nil, err
	}
	if err := os.Chdir(wd); err != nil {
		return nil, err
	}
	return func() {
		_ = os.Chdir(old)
		_ = os.RemoveAll(home)
		_ = os.RemoveAll(wd)
	}, nil
}

func TestProcessEnvIsolated(t *testing.T) {
	if home := os.Getenv("HOME"); !strings.HasPrefix(home, os.TempDir()) {
		t.Errorf("HOME 应指向临时目录: %q", home)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(wd, os.TempDir()) {
		t.Errorf("cwd 应指向临时目录: %q", wd)
	}
}
