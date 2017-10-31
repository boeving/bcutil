package rpcjs

import (
	"bufio"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"sync"

	"github.com/tinylib/msgp/msgp"
)

// 写入请求出错提示。
const msgRequest = "rpc: encoding request error:"

type msgpClientCodec struct {
	rwc     io.ReadWriteCloser
	wmu     sync.Mutex
	pending map[uint64]string // 请求方法存储
}

//
// 写入请求。
// 参数param需实现 msgp.Encodable 编码接口。
// ID/方法名与参数部分分别写入。
//
func (mc *msgpClientCodec) WriteRequest(r *rpc.Request, param interface{}) error {
	req, ok := param.(msgp.Encodable)
	if !ok {
		log.Println(msgRequest, errEncodable)
		return errEncodable
	}
	mc.wmu.Lock()
	defer mc.wmu.Unlock()

	// 缓存方法名
	mc.pending[r.Seq] = r.ServiceMethod
	head := Request{r.Seq, r.ServiceMethod}

	err := msgp.Encode(mc.rwc, head)
	if err == nil {
		err = msgp.Encode(mc.rwc, req)
		log.Println("Encode req-body done...")
	}
	if err != nil {
		log.Println(msgRequest, err)
	}
	log.Println("Encode head&req done", head, req)
	return err
}

//
// 读取响应头（ID，Error）。
// 提取方法名，设置到传入的参数r上。
//
func (mc *msgpClientCodec) ReadResponseHeader(r *rpc.Response) error {
	resp := Response{}
	err := msgp.Decode(mc.rwc, &resp)
	if err != nil {
		return err
	}
	mc.wmu.Lock()
	r.ServiceMethod = mc.pending[resp.ID]
	// 取用后删除
	delete(mc.pending, resp.ID)
	mc.wmu.Unlock()

	r.Seq = resp.ID
	r.Error = resp.Error
	return nil
}

//
// 读取响应数据主体，设置到body参数上。
//
func (mc *msgpClientCodec) ReadResponseBody(body interface{}) error {
	resp, ok := body.(msgp.Decodable)
	if !ok {
		return errDecodable
	}
	return msgp.Decode(mc.rwc, resp)
}

//
// 出错时的关闭连接（仅写入）。
//
func (mc *msgpClientCodec) Close() error {
	return mc.rwc.Close()
}

//
// NewClientCodec 创建一个msgp客户端编解码器
//
func NewClientCodec(conn io.ReadWriteCloser) rpc.ClientCodec {
	return &msgpClientCodec{
		rwc:     conn,
		pending: make(map[uint64]string),
	}
}

//
// NewClient 覆写net/rpc包同名函数。
//
func NewClient(conn io.ReadWriteCloser) *rpc.Client {
	return rpc.NewClientWithCodec(NewClientCodec(conn))
}

////////////
// 源码引用
// 复制 net/rpc 拨号代码，NewClient 调用为当前包函数。
///////////////////////////////////////////////////////////////////////////////

// Dial 普通的连接方式。
func Dial(network, address string) (*rpc.Client, error) {
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}
	return NewClient(conn), nil
}

// DialHTTP connects to an HTTP RPC server at the specified network address
// listening on the default HTTP RPC path.
func DialHTTP(network, address string) (*rpc.Client, error) {
	return DialHTTPPath(network, address, DefaultRPCPath)
}

// DialHTTPPath connects to an HTTP RPC server
// at the specified network address and path.
func DialHTTPPath(network, address, path string) (*rpc.Client, error) {
	var err error
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}
	io.WriteString(conn, "CONNECT "+path+" HTTP/1.0\n\n")

	// Require successful HTTP response
	// before switching to RPC protocol.
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	if err == nil && resp.Status == connected {
		return NewClient(conn), nil
	}
	if err == nil {
		err = errors.New("unexpected HTTP response: " + resp.Status)
	}
	conn.Close()
	return nil, &net.OpError{
		Op:   "dial-http",
		Net:  network + " " + address,
		Addr: nil,
		Err:  err,
	}
}
