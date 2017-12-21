package dcp

//////////////
// 服务端实现。
// 监听本地端口，处理任意对端发送来的数据请求。
//
// 注：
// 外部应用需要自行考虑并发的设计，比如每一个请求分派一个Go程。
// 而不只是每一个客户使用一个Go程。
//
// 注意：
// 监听套接字会被拨号方式覆盖（如果本地地址:端口相同的话）。
//
///////////////////////////////////////////////////////////////////////////////

import (
	"errors"
	"io"
	"net"
	"time"
)

var (
	errNoSender = errors.New("not set Sender handler")
)

//
// Server 一个DCP子服务。
// 处理一对端点（4元组）的数据发送（服务端）。
//
type Server struct {
	laddr, raddr net.Addr
	snd          Sender
	lastTime     time.Time
}

//
// String 服务器的字符串表示。
// 格式：本地地址|对端地址（同Client）
//
func (s *Server) String() string {
	return s.laddr.String() +
		"|" +
		s.raddr.String()
}

//
// LocalAddr 返回本地端地址。
//
func (s *Server) LocalAddr() net.Addr {
	return s.laddr
}

//
// RemoteAddr 返回本地端地址。
//
func (s *Server) RemoteAddr() net.Addr {
	return s.raddr
}

//
// Response 设置响应服务。
//
func (s *Server) Response(snd Sender) {
	s.snd = snd
}

//
// 获取响应，内部调用。
//
func (s *Server) response(res []byte) (io.Reader, error) {
	if s.snd == nil {
		return nil, errNoSender
	}
	return s.snd.NewReader(res)
}

//
// Bye 断开连系。
// 主动通知客户端服务结束。
//
func (s *Server) Bye() error {
	//
}

//
// Listener 外部连系监听器。
// 负责不同客户的服务器构造分配和数据传递。
//
type Listener struct {
	conn *net.UDPConn
	// mgr  pool
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
	return &Listener{udpc}, nil
}

//
// Accept 接收外部连系请求。
//
func (l *Listener) Accept() (*Server, error) {

}

//
// Close 关闭本地监听（结束服务）。
//
func (l *Listener) Close() error {
	//
}

//
// Addr 返回本地监听地址。
//
func (l *Listener) Addr() net.Addr {
	//
}

//
// 客户子服务池。
// 用于Accept，决定是否生成新的子服务实例（否则直接调度）。
// 它应当在客户初始连系时创建。
// key: 对端地址
//
type srvPool map[string]*Server

//
// Select 选取四元组对应的子服务。
// 无论缓存中有无目标存在，都会返回一个有效的子服务实例。
//
func (p srvPool) Select(laddr, raddr net.Addr) *Server {
	kl := laddr.String()
	kr := raddr.String()

	ss, ok := m[kl]
	// 监听集（本地）
	if !ok {
		m[kl] = make(map[string]*Server)
	}
	// 远端集
	if s, ok := ss[kr]; ok {
		return s
	}
	srv := &Server{}
	ss[kr] = srv

	return srv
}

//
// 关闭一个本地监听。
// 清除本地地址对应的全部远端存储，
// 包括拨号方式建立的连系（只要本地接收地址一致）。
//
func (p srvPool) Close(laddr *DCPAddr) {
	ss, ok := m[laddr.String()]
	if !ok {
		return
	}
	delete(m, laddr.String())
}

//
// 移除一个远端连系存储。
// 这可能在远端主动Bye之后发生，或本地主动断开。
//
func (p srvPool) Remove(laddr, raddr *DCPAddr) {
	kl := laddr.String()

	if ss, ok := m[kl]; ok {
		delete(ss, raddr.String())
	}
}

//
// 清理总集中超时的子服务存储。
// 这不是一个高效的方式，仅适用P2P端点的小连接池场景。
//
// 为尽量不影响性能，清理不是一次全部完成，而是随机间断执行。
// 仅清理有限的最大的几个集合。
//
func (p srvPool) Clean(t time.Time) int {
	//
}
