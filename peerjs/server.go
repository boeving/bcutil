//
// Package peerjs 采用JSON编解码方式的RPC模块。
// 由标准库 net/rpc 定制而来。
// 考虑执行效率和节约网络带宽，编码器采用 MessagePack（github.com/tinylib/msgp）。
//
// 主要用于轻量级的RPC交互，客户端/服务器需要协调参数与响应类型的msgp编解码。
// 注：中大型的RPC应用可考虑peerpb包（由ProtoBuf支持）。
//
package peerjs

import (
	"errors"
	"io"
	"log"
	"net/rpc"

	"github.com/tinylib/msgp/msgp"
)

var (
	errDecodable = errors.New("msgp: the data not implement DecodeMsg")
	errEncodable = errors.New("msgp: the data not implement EncodeMsg")
)

// 写入响应出错提示。
const msgResponse = "rpc: encoding response error:"

//
// Server 嵌入标准Server。
// 覆盖ServeConn方法。
//
type Server struct {
	*rpc.Server
}

//
// NewServer 创建一个使用JSON/msgp编码器的RPC服务实例。
//
func NewServer() *Server {
	return &Server{rpc.NewServer()}
}

//
// ServeConn 覆盖标准动作。
// 这样使得.Accept方法可以直接使用。
//
func (s *Server) ServeConn(conn io.ReadWriteCloser) {
	s.ServeCodec(newServerCodec(conn))
}

//
// MessagePack 的编码实现。
// 空实例可直接用于 NewServer 的创建参数。
//
type msgpServerCodec struct {
	rwc    io.ReadWriteCloser
	req    Request
	closed bool
}

func newServerCodec(conn io.ReadWriteCloser) rpc.ServerCodec {
	return &msgpServerCodec{rwc: conn}
}

//
// 读取请求数据，包括传递的参数。
// 方法名称格式：Service.Method，区分大小写。
//
func (mc *msgpServerCodec) ReadRequestHeader(r *rpc.Request) error {
	if err := msgp.Decode(mc.rwc, &mc.req); err != nil {
		return err
	}
	r.Seq = mc.req.ID
	r.ServiceMethod = mc.req.Method
	return nil
}

//
// 参数类型需要实现 msgp.Decodable 接口。
// msgp -marshal=false -file ...
//
func (mc *msgpServerCodec) ReadRequestBody(v interface{}) error {
	if v == nil {
		return nil
	}
	dec, ok := v.(msgp.Decodable)
	if !ok {
		return errDecodable
	}
	return msgp.Decode(mc.rwc, dec)
}

//
// v 结果类型需要实现 msgp.Encodable 接口。
// 要求并发安全（无此问题）。
//
func (mc *msgpServerCodec) WriteResponse(r *rpc.Response, v interface{}) error {
	body, ok := v.(msgp.Encodable)
	if !ok {
		mc.Close()
		log.Println(msgResponse, errEncodable)
		return errEncodable
	}
	head := Response{r.Seq, r.Error}
	err := msgp.Encode(mc.rwc, &head)

	if r.Error == "" && err == nil {
		err = msgp.Encode(mc.rwc, body)
	}
	// 编码/传递错误，关闭连接。
	if err != nil {
		mc.Close()
		log.Println(msgResponse, err)
	}
	return err
}

func (mc *msgpServerCodec) Close() error {
	if mc.closed {
		// Only call mc.rwc.Close once;
		// otherwise the semantics are undefined.
		return nil
	}
	mc.closed = true
	return mc.rwc.Close()
}
