package dcp

////////////////////
// DCP 内部服务实现。
//
///////////////////////////////////////////////////////////////////////////////

import (
	"net"
	"time"
)

//
// service 一个DCP子服务。
// 处理一对端点（4元组）的数据发送（服务端）。
//
type service struct {
	conn     *net.UDPConn
	lastTime time.Time // 最近连系时间戳
}
