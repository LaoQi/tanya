// Package attach 实现运行实例登记与本地接管控制协议（被接管方监听、接管方发起）。
// 只依赖标准库，不感知 tanya 内部类型；工作区域内的实例通过 run 目录互相发现。
package attach

import (
	"encoding/json"
	"net"
	"time"
)

const (
	Magic   = "tanya-attach"
	Version = 1

	HandshakeTimeout = 3 * time.Second
	WriteTimeout     = 3 * time.Second
	ProbeTimeout     = 300 * time.Millisecond
)

const (
	CodeNoSave     = "no-save"
	CodeReadOnly   = "read-only"
	CodeInProgress = "in-progress"
	CodeNoSession  = "no-session"
	CodeVersion    = "version-mismatch"
	CodeBadRequest = "bad-request"
)

type Hello struct {
	Magic string `json:"magic"`
	V     int    `json:"v"`
	PID   int    `json:"pid,omitempty"`
}

type Request struct {
	Force bool `json:"force,omitempty"`
}

type Result struct {
	OK         bool   `json:"ok"`
	Code       string `json:"code,omitempty"`
	Session    string `json:"session,omitempty"`
	File       string `json:"file,omitempty"`
	SessionDir string `json:"session_dir,omitempty"`
	ArchiveDir string `json:"archive_dir,omitempty"`
	Workspace  string `json:"workspace,omitempty"`
	Model      string `json:"model,omitempty"`
	Effort     string `json:"effort,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	Messages   int    `json:"messages,omitempty"`
}

// Peer 是接管请求的对端连接：Gone 在对端关闭连接时关闭。
type Peer struct{ gone <-chan struct{} }

func (p Peer) Gone() <-chan struct{} { return p.gone }

// Handler 由被接管方实现：Takeover 受理一次接管请求，reply 在交接点调用并阻塞到结果写出，
// 返回错误表示未送达（调用方据此回滚并继续运行）。
type Handler interface {
	Takeover(force bool, peer Peer, reply func(Result) error)
}

type message struct {
	Hello    *Hello   `json:"hello,omitempty"`
	Takeover *Request `json:"takeover,omitempty"`
	Result   *Result  `json:"result,omitempty"`
}

type conn struct {
	net.Conn
	dec *json.Decoder
	enc *json.Encoder
}

func newConn(c net.Conn) *conn {
	return &conn{Conn: c, dec: json.NewDecoder(c), enc: json.NewEncoder(c)}
}

func (c *conn) send(m message) error {
	if err := c.SetWriteDeadline(time.Now().Add(WriteTimeout)); err != nil {
		return err
	}
	return c.enc.Encode(m)
}

func (c *conn) recv() (message, error) {
	var m message
	err := c.dec.Decode(&m)
	return m, err
}
