package repl

import "testing"

func TestParseCommand(t *testing.T) {
	cases := []struct {
		name string
		args []string
		cmd  Cmd
		rest string
	}{
		{"无参数", nil, CmdREPL, ""},
		{"普通对话", []string{"hello"}, CmdREPL, ""},
		{"ask 单发", []string{"ask", "你好", "世界"}, CmdAsk, "你好 世界"},
		{"ask 无问题", []string{"ask"}, CmdAsk, ""},
		{"init", []string{"init"}, CmdInit, ""},
		{"config", []string{"config"}, CmdConfig, ""},
		{"config 多余参数", []string{"config", "-o"}, CmdConfig, "-o"},
		{"init 多余参数", []string{"init", "extra"}, CmdInit, "extra"},
		{"未知子命令按对话", []string{"Ask", "x"}, CmdREPL, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd, rest := ParseCommand(c.args)
			if cmd != c.cmd || rest != c.rest {
				t.Errorf("ParseCommand(%q) = %v,%q want %v,%q", c.args, cmd, rest, c.cmd, c.rest)
			}
		})
	}
}
