package ppnet

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
	"errors"
	"net"
)

var (
	errListenExit = errors.New("listening exit")
)

//
// Listener 外部连系监听器。
// 负责不同客户的服务器构造分配和数据传递。
//
type Listener struct {
	conn    *connReader         // 网络读取
	laddr   net.Addr            // 本地地址存储
	pool    map[string]*service // Key: 远端地址
	stopped bool                // 停止监听标记
}

//
// Listen 本地连系监听。
// 返回一个监听器实例，用于接收远端的连入（Accept）。
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
// 如果是一个新的对端，返回一个连系实例（供请求资源）。
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
		if l.stopped {
			if len(l.pool) == 0 {
				break
			}
			continue
		}
		// 来自新的对端
		return l.newContact(raddr, pack, kr), nil
	}
	return nil, errListenExit
}

//
// 创建一个新的连系。
//
func (l *Listener) newContact(raddr net.Addr, pack *packet, k string) *Contact {
	c := Contact{
		laddr: l.laddr,
		raddr: raddr,
		sends: newXSender(),
		servs: newService(&connWriter{raddr, l.conn.Conn}, l.remove),
		// rdsrv:  nil, // not needed.
	}
	l.pool[k] = c.servs
	go c.servs.Start().Post(pack)

	return &c
}

//
// Close 停止本地监听。
// 已创建连系的两个端点间仍然可正常通信。
//
func (l *Listener) Close() error {
	l.stopped = true

	if len(l.pool) == 0 {
		return l.conn.Close()
	}
	return nil
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
	if l.stopped && len(l.pool) == 1 {
		l.conn.Close()
	}
	delete(l.pool, raddr.String())
}
