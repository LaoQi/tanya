package style

import (
	"os"
	"strings"
)

type ColorLevel uint8

const (
	LevelNone ColorLevel = iota
	Level16
	Level256
	LevelTrue
)

type Profile struct {
	TTY     bool
	Colors  ColorLevel
	Unicode bool
}

var current = Profile{TTY: true, Colors: Level16, Unicode: true}

func SetProfile(p Profile) { current = p }

func GetProfile() Profile { return current }

func DetectProfile(isTTY bool) Profile {
	p := Profile{TTY: isTTY, Colors: Level16, Unicode: true}
	if !isTTY {
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
	switch {
	case os.Getenv("COLORTERM") == "truecolor", os.Getenv("WT_SESSION") != "":
		if p.Colors == Level16 {
			p.Colors = LevelTrue
		}
	case strings.Contains(os.Getenv("TERM"), "256color"):
		if p.Colors == Level16 {
			p.Colors = Level256
		}
	}
	return p
}
