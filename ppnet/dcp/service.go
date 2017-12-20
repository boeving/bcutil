package dcp

////////////////////
// DCP 内部服务实现。
//
///////////////////////////////////////////////////////////////////////////////

import (
	"io"
	"net"
	"time"
)

//
// 一个DCP子服务。
// 处理一对端点（4元组）的数据发送（服务端）。
//
type service struct {
	Conn     *net.UDPConn
	LastTime time.Time // 最近连系时间戳
}

//
// 把对端发送来的数据写入外部应用。
//
func (s *service) WriteTo(w io.Writer) (int64, error) {
	//
}

//
// 全局管理器实例。
//
var gManager = make(manager)

//
// DCP服务服务管理器。
// 用于Accept返回的连系的子服务分配。
// 分两级存储：本地监听集/远端集。
//
type manager map[string]map[string]*service

//
// Select 选取四元组对应的子服务。
// 无论缓存中有无目标存在，都会返回一个有效的子服务实例。
//
func (m manager) Select(laddr, raddr *DCPAddr) *service {
	kl := laddr.String()
	kr := raddr.String()

	ss, ok := m[kl]
	// 监听集（本地）
	if !ok {
		m[kl] = make(map[string]*service)
	}
	// 远端集
	if s, ok := ss[kr]; ok {
		return s
	}
	srv := &service{}
	ss[kr] = srv

	return srv
}

//
// 关闭一个本地监听。
// 清除对应的远端集合存储。
//
func (m manager) Close(laddr *DCPAddr) {
	delete(m, laddr.String())
}

//
// 移除一个远端连系存储。
// 这可能在远端主动Bye之后发生，或本地主动断开。
//
func (m manager) Remove(laddr, raddr *DCPAddr) {
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
func (m manager) Clean(t time.Time) int {
	//
}
