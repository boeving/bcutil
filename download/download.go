// Package download 下载器。
// 支持并发、断点续传。
// 外部实现特定的下载方式，如http直接下载或P2P传输。
// 外部定义下载响应集。
//
package download

import (
	"crypto/sha1"
	"errors"
	"io"
)

// 基本常量
const (
	SumSize   = 20       // 校验和哈希长度（sha1）
	BlockSize = 1024 * 4 // 文件分块大小（4k）
)

var (
	errSize   = errors.New("the file size or blocksize is zero")
	errUndone = errors.New("the download task undone")
	errChksum = errors.New("the file checksum is not match")
)

// HashSum sha1校验和。
type HashSum [SumSize]byte

// Status 下载状态。
type Status struct {
	Total     int64
	Completed int64
}

// Block 分段区块。
type Block struct {
	Begin int64
	End   int64
	Sum   *HashSum
}

//
// Hauler 数据搬运工。
// 实施单个目标（分块）的具体下载行为，
//
type Hauler interface {
	// 下载分块。
	Get(Block) ([]byte, error)

	// 注册校验哈希函数
	// @tries 失败尝试次数（不宜过大）
	CheckSum(sum func([]byte) HashSum, tries int)

	// 注册完成回调。
	Completed(done func([]byte))
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
// Manager 下载管理。
// 负责单个目标数据的分块，组合校验，缓存等。
//
type Manager struct {
	Size    int64           // 文件大小（bytes）
	FileSum HashSum         // 文件校验和
	Blocks  map[int64]Block // 分块集，键：Begin
	Cache   io.ReadWriter   // 分块数据缓存（临时空间）
	Indexes io.ReadWriter   // 未完成分块索引缓存

	filedata   []byte
	restIndexs map[int64]struct{}
}

//
// Resume 管理器重启。
// 主要为检测缓存状况，重构待下载分块集。
//
func (m *Manager) Resume() error {

}

//
// Divide 分块配置。
// 对数据大小进行分块定义，没有分块校验和信息。
// 一般仅在http简单下载场合使用。
//
// @bsz 分块大小
//
func (m *Manager) Divide(bsz int64) error {
	if m.Size == 0 || bsz == 0 {
		return errSize
	}
	buf := make(map[int64]Block)
	cnt, mod := m.Size/bsz, m.Size%bsz

	i := 0
	for i < cnt {
		off := i * bsz
		buf[off] = Block{off, off + bsz, nil}
		m.restIndexs[off] = struct{}{}
		i++
	}
	if mod > 0 {
		off := cnt * bsz
		buf[off] = Block{off, off + mod, nil}
		m.restIndexs[off] = struct{}{}
	}
	m.Blocks = buf

	return nil
}

//
// BlockGetter 获取一个分块范围值。
//
func (m *Manager) BlockGetter() <-chan Block {
	ch := make(chan Block, 1)

	go func() {
		for _, v := range m.Blocks {
			ch <- v
		}
		close(ch)
	}()

	return ch
}

//
// CacheBlock 缓存分块数据。
// 缓存成功后更新剩余索引区（restIndexs）。
//
func (m *Manager) CacheBlock(data []byte, b Block) (int64, error) {

}

//
// CacheIndexs 缓存剩余分块索引。
//
func (m *Manager) CacheIndexs() (int64, error) {

}

//
// Finish 下载完成。
// 返回读取接口。
// 如果文件校验和不匹配或未下载完，返回error
//
func (m *Manager) Finish() (io.Reader, error) {
	if len(m.restIndexs) != 0 {
		return nil, errUndone
	}
	if sha1.Sum(m.filedata) != m.FileSum {
		return nil, errChksum
	}
	return m.Cache, nil
}

//
// Downloader 下载器。
//
// 下载目标可能是一个URL，此时一般巍为http方式获取（httpd）。
// 也可能是文件的全局标识（Hash），此时为P2P传输（peerd）。
//
type Downloader struct {
	Monitor           // 下载响应处理
	Work    Hauler    // 数据搬运工
	Out     io.Writer // 输出目标文件
	status  Status    // 当前状态
}

//
// Start 开始或重新下载。
//
func (dl *Downloader) Start(m *Manager) {
	//
}

//
// Pause 暂停。
// 会缓存下载的文件，待后续断点续传。
//
func (dl *Downloader) Pause() {
	//
}

//
// Resume 继续下载。
// 从未完成的临时文件开始，程序中途可能退出。
//
func (dl *Downloader) Resume() {
	//
}

//
// Cancel 取消下载。
// 包含Clean逻辑，会清除下载的临时文件。
//
func (dl *Downloader) Cancel() {
	//
}

//
// Status 获取下载状态。
//
func (dl *Downloader) Status() {
	//
}

//
// Clean 清理临时文件。
//
func (dl *Downloader) Clean() bool {

}
