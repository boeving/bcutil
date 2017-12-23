package dcp

//////////////
// 客户端实现。
// 处理一对一的两个端点之间的连系。
//
// 客户端既可以发出查询请求，也可以响应对端的查询。
// 即：既充当用户，也充当服务角色。
///////////////////////////////////////////////////////////////////////////////

import (
	"errors"
	"net"
	"time"
)

var (
	errOverflow = errors.New("exceeded the number of resource queries")
)

//
// Client 通用客户端。
//
type Client struct {
	conn     *net.UDPConn
	lastTime time.Time
	pool     map[uint16]Receiver
	snd      Sender // 可选
}

//
// Dial 拨号目标地址。
// 可以传入一个指定的本地接收地址，否则系统自动配置。
// snd 参数可选。如果本地同时需要提供对端请求的数据，则可传递一个发送器。
//
func Dial(laddr, raddr *DCPAddr) (*Client, error) {
	n1 := laddr.net
	n2 := raddr.net
	if n1 != n2 {
		return nil, errNetwork
	}
	udpc, err := net.DialUDP(n1, laddr.addr, raddr.addr)
	if err != nil {
		return nil, err
	}
	c := Client{
		conn:     udpc,
		lastTime: time.Now(),
		pool:     make(map[uint16]Receiver),
	}
	return &c, nil
}

//
// String 客户端的字符串表示。
// 格式：本地地址|对端地址
//
// 主要用于端点连接池里的索引和管理。
//
func (c *Client) String() string {
	return c.LocalAddr().String() +
		"|" +
		c.RemoteAddr().String()
}

//
// LocalAddr 返回本地端地址。
//
func (c *Client) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

//
// RemoteAddr 返回对端地址。
//
func (c *Client) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

//
// Query 向服务端查询数据。
// 最多同时处理64k的目标查询，如果超出会返回errOverflow错误。
//
//  res 为目标数据的标识（哈希或格式化序列）。
//  rec 为外部接收器接口的实现。
//
func (c *Client) Query(res []byte, rec Receiver) error {
	//
}

//
// 接收服务端发送来的数据。
// 数据会写入到客户端应用（Reveiver实现者）。
//
func (c *Client) receive(id uint16, data []byte) (int64, error) {
	//
}

//
// 清除处理完毕的接收器。
//
func (c *Client) remove(k uint16) {
	delete(c.pool, k)
}

//
// Response 设置响应服务。
// 它是一对一拨号连系的本地端向对端提供数据服务的接口。
//
// 如果客户端仅是请求数据，则无需此设置。
//
func (c *Client) Response(snd Sender) {
	c.snd = snd
}

//
// Bye 断开连系。
// 无论数据是否传递完毕，都会结发送或接收。
// 未完成数据传输的中途结束会返回一个错误，记录了一些基本信息。
//
// 对端可能是一个服务器，也可能是一个普通的客户端。
//
func (c *Client) Bye() error {
	//
}
