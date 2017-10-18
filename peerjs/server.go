// Package peerjs 采用 MessagePack 编解码方式的RPC模块。
// 由标准库 net/rpc 定制而来。
package peerjs

import (
	"errors"
	"io"
	"net/rpc"

	"github.com/tinylib/msgp/msgp"
)

var (
	ErrDecodeable = errors.New("the data not implement Decodeable")
)

//
// Server 组合标准Server类型，添加一个编码器创建接口。
//
type Server struct {
	*rpc.Server
}

//
// NewServer 创建一个使用msgp编码器的RPC服务实例。
//
func NewServer() *Server {
	return &Server{rpc.NewServer()}
}

//
// ServeConn 覆盖标准动作。
// 这样使得Accept方法可以直接使用。
//
func (s *Server) ServeConn(conn io.ReadWriteCloser) {
	s.ServeCodec(NewServerCodec(conn))
}

//
// MessagePack 的编码实现。
// 空实例可直接用于 NewServer 的创建参数。
//
type MsgpServeCodec struct {
	rwc    io.ReadWriteCloser
	req    Request
	resp   Response
	closed bool
}

func NewServerCodec(conn io.ReadWriteCloser) rpc.ServerCodec {
	return &MsgpServeCodec{rwc: conn}
}

func (ms *MsgpServeCodec) ReadRequestHeader(r *rpc.Request) error {
	if err := msgp.Decode(ms.rwc, &ms.req); err != nil {
		return err
	}
	r.Seq = ms.req.Seq
	r.ServiceMethod = ms.req.ServiceMethod
	return nil
}

func (ms *MsgpServeCodec) ReadRequestBody(body interface{}) error {
	dec, ok := body.(msgp.Decodable)
	if !ok {
		return ErrDecodeable
	}
	return msgp.Decode(ms.rwc, dec)
}

//
// 要求并发安全。
//
func (ms *MsgpServeCodec) WriteResponse(*rpc.Response, interface{}) error {

}

func (ms *MsgpServeCodec) Close() error {
	if ms.closed {
		// Only call ms.rwc.Close once;
		// otherwise the semantics are undefined.
		return nil
	}
	ms.closed = true
	return ms.rwc.Close()
}
