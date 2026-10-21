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
