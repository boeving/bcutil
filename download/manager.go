// Package download 下载器。
// 支持并发、断点续传。
// 外部实现特定的下载方式，如http直接下载或P2P传输。
// 由用户定义下载响应集。
//
package download

import (
	"errors"
	"sync"
)

// 写入缓存常量。
const (
	// 磁盘刷新（Refresh）数据下限，写入后更新未下载索引。
	// 根据硬件环境选配不同的值。
	CacheSize   = 1 << (20 + iota) // 1MB
	CacheSize2                     // 2MB
	CacheSize4                     // 4MB
	CacheSize8                     // 8MB
	CacheSize16                    // 16MB
	CacheSize32                    // 32MB
	CacheSize64                    // 64MB
)

var (
	errPieces  = errors.New("the pieces hasn't data")
	errIndexer = errors.New("the index Writer is invalid")
	errExpired = errors.New("the download task already end")
	errSize    = errors.New("file size invalid")
	errWriteAt = errors.New("the Writer not support WriteAt")
	errIndex   = errors.New("the indexs not in blocks")
)

//
// Monitor 下载监控器。
// 在下载过程的各个阶段/状态触发的响应回调。
//
type Monitor interface {
	ChPause() <-chan struct{} // 获取暂停信号
	ChExit() <-chan struct{}  // 获取退出信号
	Status() *Status          // 获取状态对象

	// 下载控制
	// 返回false表示拒绝该操作。
	Start() bool  // 开始下载
	Pause() bool  // 暂停下载
	Resume() bool // 继续暂停后的下载
	Exit() bool   // 结束下载

	Errors(off int64, err error) // 错误信息递送
	Finish() error               // 完成回调
}

// UICaller 用户行为前置约束。
// 返回false否决目标行为（如：Start、Pause等）
type UICaller func(Status) bool

//
// Manager 下载管理器。
// 实现 Monitor 接口。
//  - 普通URL下载采用http方式（httpd）
//  - 文件哈希标识采用P2P传输（peerd）
//
type Manager struct {
	OnStart  UICaller // 下载开始之前
	OnPause  UICaller // 暂停之前
	OnResume UICaller // 继续之前（暂停后）
	OnExit   UICaller // 结束之前

	OnFinish func(Status)       // 下载完成之后
	OnError  func(int64, error) // 出错之后

	status  Status        // 状态暂存
	chExit  chan struct{} // 取消信号量
	chPause chan struct{} // 暂停信号量
	semu    sync.Mutex
}

//
// ChExit 取消信号。
//
func (m *Manager) ChExit() <-chan struct{} {
	m.semu.Lock()
	defer m.semu.Unlock()
	return m.chExit
}

//
// ChPause 暂停信号。
//
func (m *Manager) ChPause() <-chan struct{} {
	m.semu.Lock()
	defer m.semu.Unlock()
	return m.chPause
}

//
// Status 接口：被设置下载状态。
//
func (m *Manager) Status() *Status {
	return &m.status
}

//
// Errors 发送出错信息。
// @off 为下载失败的分片在文件中的下标位置。
//
func (m *Manager) Errors(off int64, err error) {
	if m.OnError != nil {
		m.OnError(off, err)
	}
}

//
// Start 开始下载。
// 可能从未完成的临时文件开始（程序中途退出）。
//
func (m *Manager) Start() bool {
	if m.OnStart != nil && !m.OnStart(m.status) {
		return false
	}
	m.chExit = make(chan struct{})
	m.chPause = make(chan struct{})
	close(m.chPause)

	return true
}

//
// Pause 暂停。
//
func (m *Manager) Pause() bool {
	if m.OnPause != nil && !m.OnPause(m.status) {
		return false
	}
	m.chPause = make(chan struct{})
	return true
}

//
// Resume 继续下载。
//
func (m *Manager) Resume() bool {
	if m.OnResume != nil && !m.OnResume(m.status) {
		return false
	}
	close(m.chPause)
	return true
}

//
// Exit 结束下载。
//
func (m *Manager) Exit() bool {
	if m.OnExit != nil && !m.OnExit(m.status) {
		return false
	}
	close(m.chExit)
	//
}

//
// Clean 清理临时文件。
//
func (m *Manager) Clean() error {

}
