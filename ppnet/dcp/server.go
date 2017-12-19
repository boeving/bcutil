package dcp

import "io"

//
// Server 服务器接口。
// 由提供数据服务的应用实现，
// 返回的读取器读取完毕时表示数据体结束。
//
type Server interface {
	// 参数为客户端请求的资源ID
	// 可能是一个哈希序列，或一个特定格式的资源标识。
	Reader(res []byte) (io.Reader, error)
}

// 一个服务器实现。
type server struct {
	//
}

func (s *server) Reader(res []byte) (io.Reader, error) {
	//
}

func Listen(network string, laddr *DCPAddr) (*Thread, error) {
	//
}
