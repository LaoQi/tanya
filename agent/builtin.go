package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func DispatchBuiltin(name, argsJSON string) (string, bool) {
	switch name {
	case "get_time":
		return time.Now().Format("2006-01-02 15:04:05 -0700 MST (Monday)"), true
	case "get_env":
		var args struct {
			Names []string `json:"names"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "error: 参数解析失败: " + err.Error(), true
		}
		var b strings.Builder
		for _, n := range args.Names {
			up := strings.ToUpper(n)
			if strings.Contains(up, "KEY") || strings.Contains(up, "TOKEN") ||
				strings.Contains(up, "SECRET") || strings.Contains(up, "PASS") {
				fmt.Fprintf(&b, "%s: <拒绝：疑似敏感变量>\n", n)
				continue
			}
			if v, ok := os.LookupEnv(n); ok {
				fmt.Fprintf(&b, "%s=%s\n", n, v)
			} else {
				fmt.Fprintf(&b, "%s: <未设置>\n", n)
			}
		}
		return strings.TrimRight(b.String(), "\n"), true
	case "calc":
		var args struct {
			Expression string `json:"expression"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "error: 参数解析失败: " + err.Error(), true
		}
		v, err := calcEval(args.Expression)
		if err != nil {
			return "error: " + err.Error(), true
		}
		return strconv.FormatFloat(v, 'g', -1, 64), true
	}
	return "", false
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
		return 0, fmt.Errorf("表达式存在无法解析的部分: %q", p.s[p.i:])
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
				return 0, fmt.Errorf("除数为零")
			}
			v /= r
		case '%':
			if r == 0 {
				return 0, fmt.Errorf("除数为零")
			}
			v = float64(int64(v) % int64(r))
		}
	}
}

func (p *calcParser) parseFactor() (float64, error) {
	p.skipSpace()
	if p.i >= len(p.s) {
		return 0, fmt.Errorf("表达式意外结束")
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
			return 0, fmt.Errorf("缺少右括号")
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
		return 0, fmt.Errorf("第 %d 个字符处应为数字", p.i+1)
	}
	return strconv.ParseFloat(p.s[start:p.i], 64)
}
