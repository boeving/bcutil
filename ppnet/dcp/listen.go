package dcp

///////////////
/// 服务端实现。
/// 监听本地端口，处理任意对端发送来的数据请求。
///
/// 注：
/// 外部应用需要自行考虑并发的设计，比如每一个请求分派一个Go程。
/// 而不只是每一个客户使用一个Go程。
///
/// 注意：
/// 监听套接字会被拨号方式覆盖（如果本地地址:端口相同的话）。
///////////////////////////////////////////////////////////////////////////////

import (
	"net"
	"time"
)

//
// Listener 外部连系监听器。
// 负责不同客户的服务器构造分配和数据传递。
//
type Listener struct {
	conn  *connReader
	laddr net.Addr
	pool  map[string]*service
}

//
// Listen 本地连系监听。
// 返回的连系对象仅可用于断开连系。
//
func Listen(laddr *DCPAddr) (*Listener, error) {
	udpc, err := net.ListenUDP(laddr.net, laddr.addr)
	if err != nil {
		return nil, err
	}
	l := Listener{
		conn:  &connReader{udpc},
		laddr: laddr.addr,
		pool:  make(map[string]*service),
	}
	return &l, nil
}

//
// Accept 接收外部连系请求。
// 如果是一个新的对端，返回一个处理服务器。
//
func (l *Listener) Accept() (*Contact, error) {
	for {
		pack, raddr, err := l.conn.Receive()
		if err != nil {
			return nil, err
		}
		kr := raddr.String()
		if srv, ok := l.pool[kr]; ok {
			go srv.Post(pack)
			continue
		}
		// 来自新的对端，新建连系
		c := Contact{
			laddr: l.laddr,
			raddr: raddr,
			serv:  newService(&connWriter{raddr, l.conn.Conn}, l.remove),
			alive: time.Now(),
			// rdsrv:  nil, // not needed.
		}
		l.pool[kr] = c.serv
		go c.serv.Start().Post(pack)

		return &c, nil
	}
}

//
// Close 关闭本地监听（结束服务）。
//
func (l *Listener) Close() error {
	return l.conn.Close()
}

//
// Addr 返回本地监听地址。
//
func (l *Listener) Addr() net.Addr {
	return l.laddr
}

//
// 移除一个远端连系。
// 这可能在远端主动Bye之后发生，或本地主动清理。
//
func (l *Listener) remove(raddr net.Addr) {
	delete(l.pool, raddr.String())
}
