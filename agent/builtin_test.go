package agent

import (
	"strings"
	"testing"
)

func TestCalcEval(t *testing.T) {
	cases := []struct {
		expr string
		want float64
	}{
		{"1+2", 3},
		{"2*3+4", 10},
		{"(1+2)*3", 9},
		{"-5+2", -3},
		{"10/4", 2.5},
		{"10%3", 1},
		{" 1 + 2 * ( 3 - 1 ) ", 5},
		{"2*-3", -6},
		{"0.1+0.2", 0.3},
	}
	for _, c := range cases {
		got, err := calcEval(c.expr)
		if err != nil {
			t.Errorf("calcEval(%q) 出错: %v", c.expr, err)
			continue
		}
		if got-c.want > 1e-9 || c.want-got > 1e-9 {
			t.Errorf("calcEval(%q) = %v, 期望 %v", c.expr, got, c.want)
		}
	}
}

func TestCalcEvalError(t *testing.T) {
	for _, expr := range []string{"1/0", "1+", "abc", "(1+2", "1%0", ""} {
		if _, err := calcEval(expr); err == nil {
			t.Errorf("calcEval(%q) 期望报错", expr)
		}
	}
}

func TestDispatchCalc(t *testing.T) {
	got, ok := DispatchBuiltin("calc", `{"expression":"(1+2)*3"}`)
	if !ok {
		t.Fatal("calc 未命中")
	}
	if got != "9" {
		t.Errorf("got %q, 期望 9", got)
	}
	if _, ok := DispatchBuiltin("calc", `{"expression":"1/0"}`); !ok {
		t.Fatal("calc 错误也应命中")
	}
}

func TestDispatchGetEnv(t *testing.T) {
	t.Setenv("TANYAN_TEST_VAR", "xyz")
	got, ok := DispatchBuiltin("get_env", `{"names":["TANYAN_TEST_VAR","TANYAN_NO_SUCH_XXX","MY_SECRET_KEY"]}`)
	if !ok {
		t.Fatal("get_env 未命中")
	}
	if !strings.Contains(got, "TANYAN_TEST_VAR=xyz") {
		t.Errorf("应包含已设置的变量: %q", got)
	}
	if !strings.Contains(got, "TANYAN_NO_SUCH_XXX: <未设置>") {
		t.Errorf("应标注未设置变量: %q", got)
	}
	if !strings.Contains(got, "拒绝") {
		t.Errorf("敏感变量应被拒绝: %q", got)
	}
	if strings.Contains(got, "MY_SECRET_KEY=") {
		t.Errorf("敏感变量值不应泄露: %q", got)
	}
}

func TestDispatchGetTime(t *testing.T) {
	got, ok := DispatchBuiltin("get_time", `{}`)
	if !ok {
		t.Fatal("get_time 未命中")
	}
	if len(got) < 10 {
		t.Errorf("时间格式异常: %q", got)
	}
}

func TestDispatchUnknown(t *testing.T) {
	if _, ok := DispatchBuiltin("no_such_tool", `{}`); ok {
		t.Error("未知工具不应命中")
	}
}
