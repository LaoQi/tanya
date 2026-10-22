package repl

import "testing"

func TestParseAttachArgs(t *testing.T) {
	cases := []struct {
		in   string
		want AttachArgs
		bad  bool
	}{
		{"", AttachArgs{}, false},
		{"--list", AttachArgs{List: true}, false},
		{"-l", AttachArgs{List: true}, false},
		{"--force", AttachArgs{Force: true}, false},
		{"-f", AttachArgs{Force: true}, false},
		{"--pid 1234", AttachArgs{PID: 1234}, false},
		{"--pid=1234", AttachArgs{PID: 1234}, false},
		{"--pid 1234 --force", AttachArgs{PID: 1234, Force: true}, false},
		{"--pid", AttachArgs{}, true},
		{"--pid x", AttachArgs{}, true},
		{"--pid 0", AttachArgs{}, true},
		{"--pid -5", AttachArgs{}, true},
		{"--prune", AttachArgs{Prune: true}, false},
		{"--prune --list", AttachArgs{}, true},
		{"--prune --force", AttachArgs{}, true},
		{"--prune --pid 1234", AttachArgs{}, true},
		{"--bogus", AttachArgs{}, true},
		{"extra", AttachArgs{}, true},
		{"1234", AttachArgs{PID: 1234}, false},
		{"1234 --force", AttachArgs{PID: 1234, Force: true}, false},
		{"--force 1234", AttachArgs{PID: 1234, Force: true}, false},
		{"--pid 1234 5678", AttachArgs{}, true},
		{"1234 5678", AttachArgs{}, true},
		{"1234 --pid 5678", AttachArgs{}, true},
		{"0", AttachArgs{}, true},
		{"-1", AttachArgs{}, true},
		{"12a", AttachArgs{}, true},
		{"1234x", AttachArgs{}, true},
		{"--prune 1234", AttachArgs{}, true},
		{"1234 --prune", AttachArgs{}, true},
		{"1234 --list", AttachArgs{PID: 1234, List: true}, false},
	}
	for _, c := range cases {
		got, err := ParseAttachArgs(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("%q 应报错，得 %+v", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q = %+v, %v want %+v", c.in, got, err, c.want)
		}
	}
}
