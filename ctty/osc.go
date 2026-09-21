package ctty

const (
	oscNotifyHead = "\x1b]9;"
	oscNotifyTail = "\a"
)

func oscFrame(text string) string { return oscNotifyHead + text + oscNotifyTail }
