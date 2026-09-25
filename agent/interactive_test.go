package agent

import "testing"

func TestParseRunShellArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    string
		want    runShellArgs
		wantErr bool
	}{
		{"full", `{"command":"pwd","cwd":"/tmp","timeout":30,"interactive":true}`,
			runShellArgs{Command: "pwd", Cwd: "/tmp", Timeout: 30, Interactive: true}, false},
		{"explicit false", `{"command":"sudo -S true","interactive":false}`,
			runShellArgs{Command: "sudo -S true"}, false},
		{"absent", `{"command":"pwd"}`, runShellArgs{Command: "pwd"}, false},
		{"bad json", `{bad`, runShellArgs{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseRunShellArgs(c.args)
			if (err != nil) != c.wantErr {
				t.Fatalf("parseRunShellArgs(%q) err=%v want err=%v", c.args, err, c.wantErr)
			}
			if got != c.want {
				t.Fatalf("parseRunShellArgs(%q)=%+v want %+v", c.args, got, c.want)
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
		{"default", 0, false, ShellTimeoutSec},
		{"interactive default", 0, true, ShellInteractiveTimeoutSec},
		{"explicit wins", 30, true, 30},
		{"negative treated as default", -5, false, ShellTimeoutSec},
		{"clamped to limit", 9999, false, ShellTimeoutLimit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := effectiveShellTimeout(c.explicit, c.interactive); got != c.want {
				t.Fatalf("effectiveShellTimeout(%d,%v)=%d want %d", c.explicit, c.interactive, got, c.want)
			}
		})
	}
}
