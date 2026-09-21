package repl

import "testing"

func TestRunConfigOutputsText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"原样输出", "a: 1\nb: 2\n", "a: 1\nb: 2\n"},
		{"补齐末尾换行", "a: 1", "a: 1\n"},
		{"空文本不输出", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, out, errb := initStreams(t, modeRich)
			RunConfig(st, c.in)
			if got := out.String(); got != c.want {
				t.Errorf("stdout = %q want %q", got, c.want)
			}
			if got := errb.String(); got != "" {
				t.Errorf("stderr 不应有输出: %q", got)
			}
		})
	}
}

func TestRunConfigVisibleInPlain(t *testing.T) {
	st, out, _ := initStreams(t, modePlain)
	RunConfig(st, "a: 1\n")
	if got := out.String(); got != "a: 1\n" {
		t.Errorf("plain 档应原样可见: %q", got)
	}
}
