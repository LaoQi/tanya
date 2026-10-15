package attach

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// Client 是接管方的一次控制连接。
type Client struct {
	entry Entry
	conn  *conn
}

// Open 连接注册项指向的实例并完成握手。
func Open(e Entry) (*Client, error) {
	raw, err := Dial(e.Sock)
	if err != nil {
		return nil, err
	}
	cc := newConn(raw)
	_ = cc.SetDeadline(time.Now().Add(HandshakeTimeout))
	if err := cc.send(message{Hello: &Hello{Magic: Magic, V: Version, PID: os.Getpid()}}); err != nil {
		raw.Close()
		return nil, err
	}
	m, err := cc.recv()
	if err != nil {
		raw.Close()
		return nil, fmt.Errorf(MsgPeerGone, err)
	}
	if m.Hello == nil || m.Hello.Magic != Magic {
		raw.Close()
		return nil, errors.New(MsgBadMagic)
	}
	if m.Hello.V != Version {
		raw.Close()
		return nil, fmt.Errorf(MsgBadVersion, Version, m.Hello.V)
	}
	_ = cc.SetDeadline(time.Time{})
	return &Client{entry: e, conn: cc}, nil
}

// Takeover 请求接管并阻塞等待结果：force 限时 ForceWaitTimeout（回合卡死不再无限等），
// 非 force 无超时（排队语义，取消方式仍是关闭连接）。
func (c *Client) Takeover(force bool) (Result, error) {
	if err := c.conn.send(message{Takeover: &Request{Force: force}}); err != nil {
		return Result{}, fmt.Errorf(MsgPeerGone, err)
	}
	if force {
		_ = c.conn.SetReadDeadline(time.Now().Add(ForceWaitTimeout))
	} else {
		_ = c.conn.SetReadDeadline(time.Time{})
	}
	m, err := c.conn.recv()
	if err != nil {
		if force && os.IsTimeout(err) {
			return Result{}, errors.New(MsgForceTimeout)
		}
		return Result{}, fmt.Errorf(MsgPeerGone, err)
	}
	_ = c.conn.SetReadDeadline(time.Time{})
	if m.Result == nil {
		return Result{}, errors.New(MsgBadMagic)
	}
	return *m.Result, nil
}

// State 重读注册项（等待期刷新对端状态用）。
func (c *Client) State() (Entry, error) {
	if c.entry.Path == "" {
		return Entry{}, os.ErrNotExist
	}
	return ReadEntry(c.entry.Path)
}

func (c *Client) Close() error { return c.conn.Close() }
