// Package download 下载器。
// 支持并发、断点续传。
// 外部实现特定的下载方式，如http直接下载或P2P传输。
// 由用户定义下载响应集。
//
package download

import (
	"errors"
	"io"
)

// 基本常量
const ()

var (
	errSize    = errors.New("file size invalid")
	errWriteAt = errors.New("the Writer not support WriteAt")
	errIndex   = errors.New("the indexs not in blocks")
)

// Status 下载状态。
type Status struct {
	Total     int64
	Completed int64
}

//
// Monitor 下载管理。
// 在下载过程的各个阶段/状态触发控制响应。
//
type Monitor interface {
	OnStart(s Status)   // 下载开始之前的回调
	OnPause(s Status)   // 下载暂停之后的回调
	OnResume(s Status)  // 下载继续之前的回调
	OnCancel(s Status)  // 下载取消之后的回调
	OnFinish(s Status)  // 下载完成之后的回调
	OnError(int, error) // 出错之后的回调
}

//
// Manager 下载器。
//
// 下载目标可能是一个URL，此时一般巍为http方式获取（httpd）。
// 也可能是文件的全局标识（Hash），此时为P2P传输（peerd）。
//
type Manager struct {
	Monitor                // 下载响应处理
	Work    Hauler         // 数据搬运工
	Cacher  io.WriteSeeker // 下载数据缓存/重命名输出
	status  Status         // 当前状态
}

//
// CachePut 分片数据缓存输出。
//
func (m *Manager) CachePut(bs []PieceData) (num int, err error) {
	if len(bs) == 0 {
		return
	}
	w, ok := m.cacher.(io.WriterAt)
	if !ok {
		err = errWriteAt
		return
	}
	n := 0
	for _, b := range bs {
		if n, err = w.WriteAt(b.Data, b.Offset); err != nil {
			break
		}
		num += n

		// 安全存储后移除索引
		delete(m.restIndexes[b.Offset])
	}
	return
}

//
// Start 开始或重新下载。
//
func (dl *Manager) Start() {
	//
}

//
// Pause 暂停。
// 会缓存下载的文件，待后续断点续传。
//
func (dl *Manager) Pause() {
	//
}

//
// Resume 继续下载。
// 从未完成的临时文件开始，程序中途可能退出。
//
func (dl *Manager) Resume() {
	//
}

//
// Cancel 取消下载。
// 包含Clean逻辑，会清除下载的临时文件。
//
func (dl *Manager) Cancel() {
	//
}

//
// Status 获取下载状态。
//
func (dl *Manager) Status() {
	//
}

//
// Clean 清理临时文件。
//
func (dl *Manager) Clean() bool {

}
