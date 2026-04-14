package tunnels

import (
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// ChannelConn wraps an ssh.Channel to satisfy net.Conn for ssh.NewClientConn.
// Deadlines are treated as no-ops because ssh.Channel doesn't expose per-IO deadlines.
type ChannelConn struct {
	Channel ssh.Channel
}

func (c *ChannelConn) Read(b []byte) (int, error)  { return c.Channel.Read(b) }
func (c *ChannelConn) Write(b []byte) (int, error) { return c.Channel.Write(b) }
func (c *ChannelConn) Close() error                { return c.Channel.Close() }

func (c *ChannelConn) LocalAddr() net.Addr  { return dummyAddr("jump") }
func (c *ChannelConn) RemoteAddr() net.Addr { return dummyAddr("jump") }

func (c *ChannelConn) SetDeadline(_ time.Time) error      { return nil }
func (c *ChannelConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *ChannelConn) SetWriteDeadline(_ time.Time) error { return nil }

type dummyAddr string

func (d dummyAddr) Network() string { return string(d) }
func (d dummyAddr) String() string  { return string(d) }
