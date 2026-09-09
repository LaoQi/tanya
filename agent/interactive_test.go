package agent

import "testing"

func TestToolInteractive(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args string
		want bool
	}{
		{"explicit true", "run_shell", `{"command":"sudo -S true","interactive":true}`, true},
		{"explicit false", "run_shell", `{"command":"sudo -S true","interactive":false}`, false},
		{"absent", "run_shell", `{"command":"sudo -S true"}`, false},
		{"other tool", "get_time", `{"interactive":true}`, false},
		{"bad json", "run_shell", `{bad`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := toolInteractive(c.tool, c.args); got != c.want {
				t.Fatalf("toolInteractive(%q,%q)=%v want %v", c.tool, c.args, got, c.want)
			}
		})
	}
}

func TestEffectiveShellTimeout(t *testing.T) {
	cases := []struct {
		name        string
		explicit    int
		interactive bool
		want        int
	}{
		{"default", 0, false, shellTimeoutSec},
		{"interactive default", 0, true, shellInteractiveTimeoutSec},
		{"explicit wins", 30, true, 30},
		{"negative treated as default", -5, false, shellTimeoutSec},
		{"clamped to limit", 9999, false, shellTimeoutLimit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := effectiveShellTimeout(c.explicit, c.interactive); got != c.want {
				t.Fatalf("effectiveShellTimeout(%d,%v)=%d want %d", c.explicit, c.interactive, got, c.want)
			}
		})
	}
}
