package term

import (
	"os"
	"strings"
)

type ColorLevel uint8

const (
	LevelNone ColorLevel = iota
	Level16
)

type Profile struct {
	TTY    bool
	Colors ColorLevel
}

var current = Profile{TTY: true, Colors: Level16}

func SetProfile(p Profile) { current = p }

func GetProfile() Profile { return current }

func DetectProfile(isTTY, vt bool) Profile {
	p := Profile{TTY: isTTY, Colors: Level16}
	if !isTTY || !vt {
		p.Colors = LevelNone
	}
	if os.Getenv("NO_COLOR") != "" {
		p.Colors = LevelNone
	}
	if os.Getenv("TERM") == "dumb" {
		p.Colors = LevelNone
	}
	if v := os.Getenv("TANYA_COLOR"); v != "" {
		switch strings.ToLower(v) {
		case "1", "on", "true":
			if p.Colors == LevelNone {
				p.Colors = Level16
			}
		case "0", "off", "false":
			p.Colors = LevelNone
		}
	}
	return p
}
