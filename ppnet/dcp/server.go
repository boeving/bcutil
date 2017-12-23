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
// 即：一个对端对应一个服务实例。
//
type Server struct {
	laddr, raddr net.Addr
	snd          Sender
	lastTime     time.Time
}

func newServer(laddr, raddr net.Addr) *Server {
	return &Server{
		laddr:    laddr,
		raddr:    raddr,
		snd:      nil,
		lastTime: time.Now(),
	}
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
// Register 设置响应服务。
// 非并发安全，应当在Listener:Accept返回的最初时设置。
// 注：
// 如果需要根据不同的情况改变返回的读取器，应当在snd内部实现。
// 比如针对不同的远端地址分别对待。
//
func (s *Server) Register(snd Sender) {
	s.snd = snd
}

//
// 获取响应，包内部使用。
//
func (s *Server) response(res []byte) (io.Reader, error) {
	if s.snd == nil {
		return nil, errNoSender
	}
	return s.snd.NewReader(res, s.raddr)
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
	conn  *net.UDPConn
	pool  srvPool
	laddr net.Addr
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
		conn:  udpc,
		pool:  make(srvPool),
		laddr: laddr,
	}
	return &l, nil
}

//
// Accept 接收外部连系请求。
// 如果是一个新的对端，返回一个处理服务器。
//
func (l *Listener) Accept() (*Server, error) {
	for {
		pack, raddr, err := getPacket(l.conn)
		if err != nil {
			return nil, err
		}
		srv, old := l.pool.Select(l.laddr, raddr)
		if !old {
			return srv, nil
		}
		go l.process(srv, pack)
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
// 数据报服务处理。
// 管理底层的传输逻辑（service实现）。
//
func (l *Listener) process(s *Server, p *packet) {
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
// Select 选取远端对应的子服务。
// 无论缓存中有无目标存在，都会返回一个有效的子服务实例。
// 如果是新建服务，第二个参数为假。
//
func (p srvPool) Select(laddr, raddr net.Addr) (*Server, bool) {
	kr := raddr.String()
	if s, ok := p[kr]; ok {
		return s, true
	}
	srv := newServer(laddr, raddr)
	p[kr] = srv

	return srv, false
}

//
// 移除一个远端连系。
// 这可能在远端主动Bye之后发生，或本地主动清理。
//
func (p srvPool) Remove(raddr net.Addr) {
	delete(p, raddr.String())
}

//
// 清理总集中超时的子服务存储。
// 这不是一个高效的方式，仅适用P2P端点的小连接池场景。
//
// 考虑效率，可能间断式执行检查。
// 返回清理的条目数。
//
func (p srvPool) Clean(t time.Time) int {
	//
}
