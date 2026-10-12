package attach

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

const maxSockPath = 100

// Listen 在 run 目录里为 pid 建立控制 socket（0600）；路径过长时退到临时目录，
// 实际路径由返回值给出并写进注册项。
func Listen(dir string, pid int) (net.Listener, string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", err
	}
	path := SockPath(dir, pid)
	if len(path) > maxSockPath {
		alt := filepath.Join(os.TempDir(), fmt.Sprintf("tanya-run-%d", pid))
		if err := os.MkdirAll(alt, 0o700); err != nil {
			return nil, "", err
		}
		path = SockPath(alt, pid)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, "", fmt.Errorf(MsgListen, err)
	}
	_ = os.Chmod(path, 0o600)
	return ln, path, nil
}

// Dial 连接注册项给出的 socket，不写任何字节。
func Dial(sock string) (net.Conn, error) {
	c, err := net.DialTimeout("unix", sock, ProbeTimeout)
	if err != nil {
		return nil, fmt.Errorf(MsgDial, err)
	}
	return c, nil
}

// Alive 只做连接探测：连不上即视为陈旧（不 kill、不删除）。
func Alive(e Entry) bool {
	c, err := Dial(e.Sock)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
