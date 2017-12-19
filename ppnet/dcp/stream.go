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
// Stream 流式操作句柄。
// 实现了 net.Conn 和 io.ReadFrom 接口。
//
type Stream struct {
	stream
	// ...
}

func NewStream() *Stream {
	//
}

func (s *Stream) Dial() (*Conn, error) {
	//
}

func (s *Stream) Listen() (*Conn, error) {
	//
}

//
// 内部实现（Receiver, Server）。
//
type stream struct {
	//
}

func (s *stream) NewReader(res []byte) (io.Reader, error) {
	//
}

func (s *stream) Open(id int) (Fd, error) {
	//
}

func (s *stream) Porcess(Fd, []byte) error {
	//
}

func (s *stream) Close(Fd) error {
	//
}

//
// 流式连接接口实现。
///////////////////////////////////////////////////////////////////////////////

type Conn struct {
	//
}

func (c *Conn) Read(b []byte) (int, error) {
	//
}

//
// ReadFrom 实现 io.ReadFrom 接口。
// 从读取源 r 中读取数据，直到遇到 io.EOF 或其它错误。
//
func (c *Conn) ReadFrom(r io.Reader) (int64, error) {

}

func (c *Conn) SetReadBuffer(bytes int) error {
	//
}

func (c *Conn) Write(b []byte) (int, error) {
	//
}

func (c *Conn) SetWriteBuffer(bytes int) error {
	//
}

func (c *Conn) Close() error {
	//
}

func (c *Conn) SetDeadline(t time.Time) error {
	//
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	//
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	//
}

func (c *Conn) LocalAddr() net.Addr {
	//
}

func (c *Conn) RemoteAddr() net.Addr {
	//
}
