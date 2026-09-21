package repl

import "strings"

func RunConfig(st *streams, text string) {
	if text == "" {
		return
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	st.Print(text)
}
