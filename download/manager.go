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
// Manager 下载器。
//
// 下载目标可能是一个URL，此时一般巍为http方式获取（httpd）。
// 也可能是文件的全局标识（Hash），此时为P2P传输（peerd）。
//
type Manager struct {
	Monitor           // 下载响应处理
	Work    Hauler    // 数据搬运工
	Out     io.Writer // 输出目标文件
	status  Status    // 当前状态
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
