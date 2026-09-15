package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type builtinTool struct {
	name   string
	desc   string
	params string
	run    func(argsJSON string) string
}

func (b builtinTool) Name() string { return b.name }

func (b builtinTool) Definition() ToolDef {
	return newToolDef(b.name, b.desc, b.params)
}

func (b builtinTool) Invoke(_ context.Context, argsJSON string) ToolResult {
	return ToolResult{Text: b.run(argsJSON)}
}

func builtinTools() []Tool {
	return []Tool{
		builtinTool{
			name:   "get_time",
			desc:   "获取当前日期时间（含时区）",
			params: `{"type":"object","properties":{}}`,
			run: func(string) string {
				return time.Now().Format("2006-01-02 15:04:05 -0700 MST (Monday)")
			},
		},
		builtinTool{
			name:   "get_env",
			desc:   "获取指定环境变量的值（疑似敏感的变量会被拒绝）",
			params: `{"type":"object","properties":{"names":{"type":"array","items":{"type":"string"},"description":"环境变量名列表"}},"required":["names"]}`,
			run:    runGetEnv,
		},
		builtinTool{
			name:   "calc",
			desc:   "计算四则运算表达式，支持 + - * / % 与括号",
			params: `{"type":"object","properties":{"expression":{"type":"string","description":"算数表达式，如 (1+2)*3/4"}},"required":["expression"]}`,
			run:    runCalc,
		},
	}
}

func runGetEnv(argsJSON string) string {
	var args struct {
		Names []string `json:"names"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf(MsgParseArgs, err)
	}
	var b strings.Builder
	for _, n := range args.Names {
		up := strings.ToUpper(n)
		if strings.Contains(up, "KEY") || strings.Contains(up, "TOKEN") ||
			strings.Contains(up, "SECRET") || strings.Contains(up, "PASS") {
			fmt.Fprintf(&b, MsgEnvDenied+"\n", n)
			continue
		}
		if v, ok := os.LookupEnv(n); ok {
			fmt.Fprintf(&b, "%s=%s\n", n, v)
		} else {
			fmt.Fprintf(&b, MsgEnvUnset+"\n", n)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func runCalc(argsJSON string) string {
	var args struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf(MsgParseArgs, err)
	}
	v, err := calcEval(args.Expression)
	if err != nil {
		return "error: " + err.Error()
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

type calcParser struct {
	s string
	i int
}

func calcEval(s string) (float64, error) {
	p := &calcParser{s: s}
	v, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if p.i != len(p.s) {
		return 0, fmt.Errorf(MsgCalcUnparsed, p.s[p.i:])
	}
	return v, nil
}

func (p *calcParser) skipSpace() {
	for p.i < len(p.s) && unicode.IsSpace(rune(p.s[p.i])) {
		p.i++
	}
}

func (p *calcParser) parseExpr() (float64, error) {
	v, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpace()
		if p.i >= len(p.s) || (p.s[p.i] != '+' && p.s[p.i] != '-') {
			return v, nil
		}
		op := p.s[p.i]
		p.i++
		r, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			v += r
		} else {
			v -= r
		}
	}
}

func (p *calcParser) parseTerm() (float64, error) {
	v, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpace()
		if p.i >= len(p.s) || (p.s[p.i] != '*' && p.s[p.i] != '/' && p.s[p.i] != '%') {
			return v, nil
		}
		op := p.s[p.i]
		p.i++
		r, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		switch op {
		case '*':
			v *= r
		case '/':
			if r == 0 {
				return 0, fmt.Errorf(MsgCalcDivZero)
			}
			v /= r
		case '%':
			if r == 0 {
				return 0, fmt.Errorf(MsgCalcDivZero)
			}
			v = float64(int64(v) % int64(r))
		}
	}
}

func (p *calcParser) parseFactor() (float64, error) {
	p.skipSpace()
	if p.i >= len(p.s) {
		return 0, fmt.Errorf(MsgCalcUnexpectedEnd)
	}
	c := p.s[p.i]
	switch {
	case c == '(':
		p.i++
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		p.skipSpace()
		if p.i >= len(p.s) || p.s[p.i] != ')' {
			return 0, fmt.Errorf(MsgCalcMissingRParen)
		}
		p.i++
		return v, nil
	case c == '-':
		p.i++
		v, err := p.parseFactor()
		return -v, err
	case c == '+':
		p.i++
		return p.parseFactor()
	}
	start := p.i
	for p.i < len(p.s) && (unicode.IsDigit(rune(p.s[p.i])) || p.s[p.i] == '.') {
		p.i++
	}
	if start == p.i {
		return 0, fmt.Errorf(MsgCalcWantNumber, p.i+1)
	}
	return strconv.ParseFloat(p.s[start:p.i], 64)
}
