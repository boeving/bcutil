// Package download 分片下载模块。
// 支持并发、断点续传。
// 外部实现特定的下载方式，如http直接下载或P2P传输。
// 由用户定义下载响应集。
//
// 对于http方式的资源，也应当构建一个分片索引集，包含验证哈希。
// 这样便于P2P传输。http源被视为一个种子。
//
package download

import (
	"crypto/sha256"
	"io"
	"sync"

	"github.com/qchen-zh/pputil/download/piece"
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

//
// Controller 下载控制。
//
type Controller interface {
	// 退出控制
	// 返回的通道关闭表示退出。
	ChExit() <-chan struct{}

	// 暂停控制
	// 返回的通道阻塞表示暂停。
	ChPause() <-chan struct{}
}

//
// Monitor 下载监控器。
// 在下载过程的各个阶段/状态触发的响应回调。
//
type Monitor interface {
	Controller

	// 下载控制
	// 返回false表示拒绝该操作。
	Start() bool   // 开始下载
	Pause() bool   // 暂停下载
	Resume() bool  // 继续暂停后的下载
	Exit() bool    // 结束下载
	Finish() error // 完成下载

	// 错误信息递送
	// err 可能是piece.PieError类型。
	Errors(err error)
}

// UICaller 用户行为前置约束。
// 返回false否决目标行为（如：Start、Pause等）
type UICaller = func() bool

//
// Manager 下载管理器。
// 实现 Monitor 接口。
//  - 普通URL下载采用http方式（httpd）
//  - 文件哈希标识采用P2P传输（peerd）
//
type Manager struct {
	OnStart  UICaller     // 下载开始之前
	OnPause  UICaller     // 暂停之前
	OnResume UICaller     // 继续之前（暂停后）
	OnExit   UICaller     // 结束之前
	OnFinish func() error // 下载完成之后
	OnError  func(error)  // 出错之后

	chExit  chan struct{} // 取消信号量
	chPause chan struct{} // 暂停信号量
	semu    sync.Mutex
}

//
// ChExit 取消信号。
//
func (m *Manager) ChExit() <-chan struct{} {
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
// Start 开始下载。
// 注册回调返回false，表示不同意开始。
//
func (m *Manager) Start() bool {
	m.semu.Lock()
	defer m.semu.Unlock()

	if m.OnStart != nil && !m.OnStart() {
		return false
	}
	m.chExit = make(chan struct{})
	m.chPause = make(chan struct{})
	// non-blocking
	close(m.chPause)

	return true
}

//
// Pause 暂停。
// 返回true表示成功执行暂停。
//
func (m *Manager) Pause() bool {
	m.semu.Lock()
	defer m.semu.Unlock()

	if m.OnPause != nil && !m.OnPause() {
		return false
	}
	m.chPause = make(chan struct{})
	return true
}

//
// Resume 继续下载。
// 返回true表示成功开启继续下载。
//
func (m *Manager) Resume() bool {
	m.semu.Lock()
	defer m.semu.Unlock()

	if m.OnResume != nil && !m.OnResume() {
		return false
	}
	close(m.chPause)
	return true
}

//
// Exit 结束下载。
// 返回true表示成功执行结束（未被外部阻挡）。
//
func (m *Manager) Exit() bool {
	if m.OnExit != nil && !m.OnExit() {
		return false
	}
	close(m.chExit)
	return true
}

//
// Finish 完成后回调。
// 可能用于外部状态显示（用户）。
//
func (m *Manager) Finish() error {
	if m.OnFinish != nil {
		return m.OnFinish()
	}
	return nil
}

//
// Errors 发送出错信息。
// @off 为下载失败的分片在文件中的下标位置。
//
func (m *Manager) Errors(err error) {
	if m.OnError != nil {
		m.OnError(err)
	}
}

//
// CheckSum 计算完整的哈希校验和。
// 外部可传入一个新打开的文件句柄，或其它输入流。
//
// 如果网络连接和速度不是问题（如本机服务器），
// 这也可用于直接校验URL的网络文件，而无需先下载存储。
//
func CheckSum(r io.Reader) (piece.HashSum, error) {
	h := sha256.New()
	sum := piece.HashSum{}

	if _, err := io.Copy(h, r); err != nil {
		return sum, err
	}
	copy(sum[:], h.Sum(nil))

	return sum, nil
}
