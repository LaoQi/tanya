package attach

import (
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Server 在被接管进程内监听控制 socket，并把请求交给 Handler。
type Server struct {
	ln     net.Listener
	sock   string
	dir    string
	tmpDir string
	pid    int
	h      Handler

	mu   sync.Mutex
	stop bool
}

func Serve(dir string, pid int, h Handler) (*Server, error) {
	ln, sock, err := Listen(dir, pid)
	if err != nil {
		return nil, err
	}
	s := &Server{ln: ln, sock: sock, dir: dir, pid: pid, h: h}
	if filepath.Dir(sock) != dir {
		s.tmpDir = filepath.Dir(sock)
	}
	go s.accept()
	return s, nil
}

func (s *Server) SockPath() string { return s.sock }

// Close 关闭监听、删除 socket 与注册项（幂等）；已在处理中的连接自行退出。
func (s *Server) Close() error {
	s.mu.Lock()
	if s.stop {
		s.mu.Unlock()
		return nil
	}
	s.stop = true
	s.mu.Unlock()
	err := s.ln.Close()
	_ = os.Remove(s.sock)
	if s.tmpDir != "" {
		_ = os.Remove(s.tmpDir)
	}
	_ = RemoveEntry(s.dir, s.pid)
	return err
}

func (s *Server) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(c)
	}
}

func (s *Server) serve(c net.Conn) {
	defer c.Close()
	cc := newConn(c)
	_ = cc.SetDeadline(time.Now().Add(HandshakeTimeout))
	m, err := cc.recv()
	if err != nil || m.Hello == nil || m.Hello.Magic != Magic {
		return
	}
	if err := cc.send(message{Hello: &Hello{Magic: Magic, V: Version, PID: os.Getpid()}}); err != nil {
		return
	}
	if m.Hello.V != Version {
		return
	}
	_ = cc.SetReadDeadline(time.Now().Add(HandshakeTimeout))
	m, err = cc.recv()
	if err != nil || m.Takeover == nil {
		return
	}
	_ = cc.SetReadDeadline(time.Time{})
	gone := watchGone(c)
	s.h.Takeover(m.Takeover.Force, Peer{gone: gone}, func(res Result) error {
		return cc.send(message{Result: &res})
	})
}

// watchGone 监视对端断开：接管方取消等待即关闭连接，被接管方据此放弃交接。
func watchGone(c net.Conn) <-chan struct{} {
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		buf := make([]byte, 1)
		for {
			if _, err := c.Read(buf); err != nil {
				return
			}
		}
	}()
	return gone
}
