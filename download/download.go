// Package download 下载器。
// 支持并发、断点续传。
// 外部实现特定的下载方式，如http直接下载或P2P传输。
// 外部定义下载响应集。
//
package download

import (
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/qchen-zh/pputil/goes"
)

// 基本常量
const (
	SumSize   = 20       // 校验和哈希长度（sha1）
	BlockSize = 1024 * 4 // 文件分块大小（4k）

	// 构造分块校验和并发量
	// 行为：读取分块，Hash计算
	SumThread = 12
)

const (
	// 分块索引单元长度
	lenBlockIdx = 8 + 8 + 20
)

var (
	errSize    = errors.New("file size invalid")
	errWriteAt = errors.New("the Writer not support WriteAt")
	errIndex   = errors.New("the indexs not in blocks")
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
// Read 读取设置分块定义。
// 数据结构：
//  Begin[8]End[8]Sum[20] = 36 bytes
//  大端字节序
//
func (b *Block) Read(buf [lenBlockIdx]byte) {
	b.Begin = int64(binary.BigEndian.Uint64(buf[:8]))
	b.End = int64(binary.BigEndian.Uint64(buf[8:16]))

	b.Sum = new(HashSum)
	copy((*b.Sum)[:], buf[16:])
}

//
// Bytes 编码分块定义为序列。
//
func (b *Block) Bytes() []byte {
	var buf [lenBlockIdx]byte

	binary.BigEndian.PutUint64(buf[:8], Uint64(b.Begin))
	binary.BigEndian.PutUint64(buf[8:16], Uint64(b.End))

	copy(buf[16:], (*b.Sum)[:])
	return buf[:]
}

//
// BlockData 分块数据
// 注：用于存储。
//
type BlockData struct {
	Offset int64
	Data   []byte
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
	Size    int64            // 文件大小（bytes）
	Blocks  map[int64]*Block // 分块集，键：Begin
	Cacher  io.Writer        // 分块数据缓存
	Indexer io.ReadWriter    // 未完成分块索引存储

	restIndexes map[int64]struct{}
}

//
// IndexesRest 构建剩余分块索引。
// 依Blocks成员构建，在Blocks赋值之后调用。
//
func (m *Manager) IndexesRest() {
	if m.Blocks == nil {
		return
	}
	buf := make(map[int64]struct{}, len(m.Blocks))

	for off := range m.Blocks {
		buf[off] = struct{}{}
	}
	m.restIndexes = buf
}

//
// LoadIndexes 载入待下载分块索引。
//
// 后续续传时需要，Blocks成员无需初始赋值。
// 需要读取Indexer成员，应当已经赋值。
//
// 之后应当调用IndexRest构建剩余分块索引信息。
//
func (m *Manager) LoadIndexes() error {
	if m.Blocks == nil {
		m.Blocks = make(map[int64]*Block)
	}
	var buf [lenBlockIdx]byte
	for {
		if _, err := io.ReadFull(m.Indexer, buf[:]); err != nil {
			return fmt.Errorf("cache index invalid: %v", err)
		}
		var b Block
		b.Read(buf)
		m.Blocks[b.Begin] = &b
	}
	return nil
}

//
// Divide 分块配置。
//
// 对数据大小进行分块定义，没有分块校验和信息。
// 一般仅在http简单下载场合使用。
// 之后应当调用IndexRest构建剩余分块索引信息。
//
//  @bsz 分块大小，应大于零。零值引发panic
//
func (m *Manager) Divide(bsz int64) error {
	buf, err := Divide(m.Size, bsz)
	if err != nil {
		return err
	}
	m.Blocks = buf
	return nil
}

//
// BlockGetter 获取分块定义取值渠道。
//
func (m *Manager) BlockGetter() <-chan Block {
	ch := make(chan Block, 1)

	go func() {
		for _, v := range m.Blocks {
			ch <- *v
		}
		close(ch)
	}()

	return ch
}

//
// SaveCache 缓存分块数据。
// 数据缓存成功后更新索引区（restIndexes）。
//
func (m *Manager) SaveCache(bs []BlockData) (num int, err error) {
	if len(bs) == 0 {
		return
	}
	w, ok := m.Cacher.(io.WriterAt)
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
// SaveIndexs 缓存剩余分块索引。
//
func (m *Manager) SaveIndexs() (int, error) {
	if len(m.restIndexes) == 0 {
		return 0, nil
	}
	n := 0
	buf := make([]byte, 0, len(m.restIndexes)*lenBlockIdx)

	for off := range m.restIndexes {
		b := m.Blocks[off]
		if b == nil {
			return 0, errIndex
		}
		buf = append(buf, b.Bytes())
	}

	return m.Indexer.Write(buf)
}

//
// Divide 分块配置。
//
// 对数据大小进行分块定义，没有分块校验和信息。
// 一般仅在http简单下载场合使用。
// 之后应当调用IndexRest构建剩余分块索引信息。
//
// 	@size 文件大小，应大于零。
// 	@bsz  分块大小，应大于零。零值引发panic
//
func Divide(size, bsz int64) (map[int64]*Block, error) {
	if size <= 0 {
		return nil, errSize
	}
	if bsz <= 0 {
		return panic("block size invalid")
	}
	buf := make(map[int64]*Block)
	cnt, mod := size/bsz, size%bsz

	var i int64
	for i < cnt {
		off := i * bsz
		buf[off] = &Block{off, off + bsz, nil}
		i++
	}
	if mod > 0 {
		off := cnt * bsz
		buf[off] = &Block{off, off + mod, nil}
	}
	return buf, nil
}

//
// BlockSumor 分块校验和生成器。
// 按分块定义读取目标数据，计算校验和并设置。
//
type BlockSumor struct {
	r    io.ReaderAt
	data map[int64]*Block
	ch   chan interface{}
}

//
// NewBlockSumor 新建一个分块校验和生成器。
// list清单为针对r的源文件大小分割而来。
// 分块校验和设置在list成员的Sum字段上。
//
// 返回值仅可单次使用，由 Do|FullDo 实施。
// list是一个引用，外部不应该再修改它。
//
func NewBlockSumor(r io.ReaderAt, list map[int64]*Block) *BlockSumor {
	bs := BlockSumor{
		r:    r,
		data: list,
	}
	bs.ch = make(chan interface{})

	// 启动一个服务
	go func() {
		for k := range bs.data {
			bs.ch <- k
		}
		close(ch) // for nil
	}()

	return &bs
}

//
// Task 获取一个分块定义。
//
func (bs *BlockSumor) Task() interface{} {
	return <-bs.ch
}

//
// Work 计算一个分块数据的校验和。
//
func (bs *BlockSumor) Work(k interface{}) error {
	b := bs.data[k.(int64)]
	if b == nil {
		return errIndex
	}
	data, err := blockRead(bs.r, b.Begin, b.End)

	if err != nil {
		return err
	}
	b.Sum = new(HashSum)
	copy((*b.Sum)[:], sha1.Sum(buf))

	return nil
}

//
// Do 计算/设置分块校验和（有限并发）。
//
// 启动 limit 个并发计算，传递0采用内置默认值（SumThread）。
// 返回的等待对象用于等待全部工作完成。
//
// 应用需要处理bad内的值，并且需要清空。
//
func (bs *BlockSumor) Do(limit int, bad chan<- error, cancel func() bool) *sync.WaitGroup {
	if limit <= 0 {
		limit = SumThread
	}
	return goes.Works(goes.LimitTasker(bs, limit), bad, cancel)
}

//
// FullDo 计算/设置分块校验和（完全并发）。
//
// 有多少个分块启动多少个协程。其它说明与Do相同。
// 注：通常仅在分块较大数量较少时采用。
//
func (bs *BlockSumor) FullDo(bad chan<- error, cancel func() bool) *sync.WaitGroup {
	return goes.Works(bs, bad, cancel)
}

//
// 读取特定片区的数据。
// 读取出错后返回错误（通知外部）。
//
func blockRead(r io.ReaderAt, begin, end int64) ([]byte, error) {
	buf := make([]byte, begin-end)
	n, err := r.ReadAt(buf, begin)

	if n < len(buf) {
		return nil, err
	}
	return buf, nil
}

//
// CheckSum 整体数据校验。
//
func CheckSum(r io.Reader, cksum HashSum) bool {
	h := sha1.New()
	if _, err := io.Copy(h, r); err != nil {
		return false
	}
	if h.Sum(nil) != cksum {
		return false
	}
	return true
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
