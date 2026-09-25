package shell

import "testing"

func TestParseRunShellArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    string
		want    runArgs
		wantErr bool
	}{
		{"full", `{"command":"pwd","cwd":"/tmp","timeout":30,"interactive":true}`,
			runArgs{Command: "pwd", Cwd: "/tmp", Timeout: 30, Interactive: true}, false},
		{"explicit false", `{"command":"sudo -S true","interactive":false}`,
			runArgs{Command: "sudo -S true"}, false},
		{"absent", `{"command":"pwd"}`, runArgs{Command: "pwd"}, false},
		{"bad json", `{bad`, runArgs{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseRunArgs(c.args)
			if (err != nil) != c.wantErr {
				t.Fatalf("parseRunArgs(%q) err=%v want err=%v", c.args, err, c.wantErr)
			}
			if got != c.want {
				t.Fatalf("parseRunArgs(%q)=%+v want %+v", c.args, got, c.want)
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
		{"default", 0, false, TimeoutSec},
		{"interactive default", 0, true, InteractiveTimeoutSec},
		{"explicit wins", 30, true, 30},
		{"negative treated as default", -5, false, TimeoutSec},
		{"clamped to limit", 9999, false, TimeoutLimit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := effectiveTimeout(c.explicit, c.interactive); got != c.want {
				t.Fatalf("effectiveTimeout(%d,%v)=%d want %d", c.explicit, c.interactive, got, c.want)
			}
		})
	}
}
