package repl

import (
	"strings"
	"testing"

	"github.com/LaoQi/tanya/readline"
)

func TestRunExitPathsUnified(t *testing.T) {
	cases := []struct {
		name string
		term *fakeTerm
	}{
		{"EOF", newFakeTerm()},
		{"exit 行", newFakeTerm(line("exit"))},
		{"外部退出请求", &fakeTerm{err: readline.ErrExited}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, out, errb := newTestREPLAgent(t, newSettleAgent(t), tc.term)
			if err := r.Run(); err != nil {
				t.Fatalf("退出应返回 nil: %v", err)
			}
			got := out.String()
			if !strings.Contains(got, "会话") || !strings.Contains(got, "时长") {
				t.Errorf("应走 farewell 收尾: %q", got)
			}
			if errb.String() != "" {
				t.Errorf("退出不应写 stderr: %q", errb.String())
			}
		})
	}
}
