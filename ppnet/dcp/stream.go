package dcp

import (
	"io"
	"net"
	"time"
)

////////////
// 流式转换器。
// 从数据包回调服务转为传统的类TCP流式逻辑。
// 内部管理一个缓存，算是一个内部系统级的子应用。
// ////////////////////////////////////////////////////////////////////////////

//
// 流式窗口服务。
//
type winServ struct {
}

func (s *winServ) NewReader(res []byte) (io.Reader, error) {
	//
}

func (s *winServ) Receive(data []byte) error {
	//
}

//
// StreamListener 流式监听器。
//
type StreamListener struct {
	l *Listener
}

func DialStream(network string, laddr, raddr *DCPAddr) (*SConn, error) {
	//
}

func ListenStream(network string, laddr *DCPAddr) (*StreamListener, error) {
	//
}

//
// Accept 接收外部连系请求。
//
func (s *StreamListener) Accept() (*Contact, error) {

}

//
// Close 关闭本地监听。
//
func (s *StreamListener) Close() error {
	//
}

//
// Addr 返回本地监听地址。
//
func (s *StreamListener) Addr() net.Addr {
	//
}

//
// 流式连接接口实现。
///////////////////////////////////////////////////////////////////////////////

type SConn struct {
}

func (c *SConn) Read(b []byte) (int, error) {
	//
}

//
// ReadFrom 实现 io.ReadFrom 接口。
// 从读取源 r 中读取数据，直到遇到 io.EOF 或其它错误。
//
func (c *SConn) ReadFrom(r io.Reader) (int64, error) {

}

func (c *SConn) SetReadBuffer(bytes int) error {
	//
}

func (c *SConn) Write(b []byte) (int, error) {
	//
}

func (c *SConn) SetWriteBuffer(bytes int) error {
	//
}

func (c *SConn) Close() error {
	//
}

func (c *SConn) SetDeadline(t time.Time) error {
	//
}

func (c *SConn) SetReadDeadline(t time.Time) error {
	//
}

func (c *SConn) SetWriteDeadline(t time.Time) error {
	//
}

func (c *SConn) LocalAddr() net.Addr {
	//
}

func (c *SConn) RemoteAddr() net.Addr {
	//
}
