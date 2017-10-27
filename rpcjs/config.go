package peerjs

import "errors"

// Defaults used by HandleHTTP
const (
	DefaultRPCPath   = "/_msgpRPC"
	DefaultDebugPath = "/debug/rpc"
)

// DefaultServer is the default instance of *Server.
var DefaultServer = NewServer()

//
// Can connect to RPC service using HTTP CONNECT to rpcPath.
// 注：此为net/rpc:Server.ServeHTTP 写入的值，因Server嵌入复用，故需保持一致。
//
var connected = "200 Connected to Go RPC"

var (
	errDecodable = errors.New("msgp: the argument not implement DecodeMsg")
	errEncodable = errors.New("msgp: the argument not implement EncodeMsg")
)

//
// Request 客户端向服务器的请求条目。
// ID强制要求为一个数字以简化逻辑。
// 方法参数可自由定制，与服务器端的参数要求对应（msgp.Decodable）。
//
// 客户端的请求数据包含两段：
//  1. ID和方法名部分，设定回应标识并定位调用目标；
//  2. 方法参数部分，通常由msgp.Encode生成字节序列，并附加在前段之后。
// 注：
// 浏览器JS环境可以使用Uint8Array类型连接该两段编码。
//
type Request struct {
	ID     uint64 `msg:"id"`     // 请求序列号，强制为数字
	Method string `msg:"method"` // 格式： "Service.Method"
}

//
// 优化：
// RPC服务请求读，减少内存分配。
//
func (r *Request) reset() {
	r.ID = 0
	r.Method = ""
}

//
// Response 服务器对客户端发送的响应头。
// 注：具体的响应数据在其自身文件中定义，需支持 msgp.Encodable 接口。
//
type Response struct {
	ID    uint64 `msg:"id"`    // 对应请求的序列号
	Error string `msg:"error"` // 非空会导致无响应数据且关闭连接
}

//
// 优化：
// RPC服务响应写，减少内存分配。
//
func (r *Response) set(id uint64, err string) {
	r.ID = id
	r.Error = err
}
