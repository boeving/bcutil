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
	SumLength = 20     // 校验和哈希长度（sha1）
	PieceUnit = 1 < 14 // 分片单位值（16k）
	PieceSize = 1 < 4  // 分片大小（16），总长16*16k = 256k

	// 构造分片校验和并发量
	// 行为：读取分片，Hash计算
	SumThread = 12
)

const (
	// 分片索引单元长度
	lenPieceIdx = 8 + 8 + 20
)

var (
	errSize    = errors.New("file size invalid")
	errWriteAt = errors.New("the Writer not support WriteAt")
	errIndex   = errors.New("the indexs not in blocks")
)

// HashSum sha1校验和。
type HashSum [SumLength]byte

// Status 下载状态。
type Status struct {
	Total     int64
	Completed int64
}

// Piece 数据分片。
type Piece struct {
	Begin int64
	End   int64
	Sum   *HashSum
}

//
// Read 读取设置分片定义。
// 数据结构：
//  Begin[8]End[8]Sum[20] = 36 bytes
//  大端字节序
//
func (b *Piece) Read(buf [lenPieceIdx]byte) {
	b.Begin = int64(binary.BigEndian.Uint64(buf[:8]))
	b.End = int64(binary.BigEndian.Uint64(buf[8:16]))

	b.Sum = new(HashSum)
	copy((*b.Sum)[:], buf[16:])
}

//
// Bytes 编码分片定义为序列。
//
func (b *Piece) Bytes() []byte {
	var buf [lenPieceIdx]byte

	binary.BigEndian.PutUint64(buf[:8], Uint64(b.Begin))
	binary.BigEndian.PutUint64(buf[8:16], Uint64(b.End))

	copy(buf[16:], (*b.Sum)[:])
	return buf[:]
}

//
// PieceData 分片数据
// 注：用于存储。
//
type PieceData struct {
	Offset int64
	Data   []byte
}

//
// Hauler 数据搬运工。
// 实施单个目标（分片）的具体下载行为，
//
type Hauler interface {
	// 下载分片。
	Get(Piece) ([]byte, error)

	// 注册校验哈希函数
	CheckSum(cksum func([]byte) HashSum)

	// 注册完成回调。
	Completed(func([]byte))

	// 注册失败回调。
	Failed(func(Piece, error))
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
// 负责单个目标数据的分片，组合校验，缓存等。
//
type Manager struct {
	Size    int64            // 文件大小（bytes）
	Pieces  map[int64]*Piece // 分片集，键：Begin
	Cacher  io.Writer        // 分片数据缓存
	Indexer io.ReadWriter    // 未完成分片索引存储

	restIndexes map[int64]struct{}
}

//
// IndexesRest 构建剩余分片索引。
// 依Pieces成员构建，在Pieces赋值之后调用。
//
func (m *Manager) IndexesRest() {
	if m.Pieces == nil {
		return
	}
	buf := make(map[int64]struct{}, len(m.Pieces))

	for off := range m.Pieces {
		buf[off] = struct{}{}
	}
	m.restIndexes = buf
}

//
// LoadIndexes 载入待下载分片索引。
//
// 后续续传时需要，Pieces成员无需初始赋值。
// 需要读取Indexer成员，应当已经赋值。
//
// 之后应当调用IndexRest构建剩余分片索引信息。
//
func (m *Manager) LoadIndexes() error {
	if m.Pieces == nil {
		m.Pieces = make(map[int64]*Piece)
	}
	var buf [lenPieceIdx]byte
	for {
		if _, err := io.ReadFull(m.Indexer, buf[:]); err != nil {
			return fmt.Errorf("cache index invalid: %v", err)
		}
		var b Piece
		b.Read(buf)
		m.Pieces[b.Begin] = &b
	}
	return nil
}

//
// Divide 分片配置。
//
// 对数据大小进行分片定义，没有分片校验和信息。
// 一般仅在http简单下载场合使用。
// 之后应当调用IndexRest构建剩余分片索引信息。
//
//  @bsz 分片大小，应大于零。零值引发panic
//
func (m *Manager) Divide(bsz int64) error {
	buf, err := Divide(m.Size, bsz)
	if err != nil {
		return err
	}
	m.Pieces = buf
	return nil
}

//
// PieceGetter 获取分片定义取值渠道。
//
func (m *Manager) PieceGetter() <-chan Piece {
	ch := make(chan Piece, 1)

	go func() {
		for _, v := range m.Pieces {
			ch <- *v
		}
		close(ch)
	}()

	return ch
}

//
// SaveCache 缓存分片数据。
// 数据缓存成功后更新索引区（restIndexes）。
//
func (m *Manager) SaveCache(bs []PieceData) (num int, err error) {
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
// SaveIndexs 缓存剩余分片索引。
//
func (m *Manager) SaveIndexs() (int, error) {
	if len(m.restIndexes) == 0 {
		return 0, nil
	}
	n := 0
	buf := make([]byte, 0, len(m.restIndexes)*lenPieceIdx)

	for off := range m.restIndexes {
		b := m.Pieces[off]
		if b == nil {
			return 0, errIndex
		}
		buf = append(buf, b.Bytes())
	}

	return m.Indexer.Write(buf)
}

//
// Divide 分片配置。
//
// 对数据大小进行分片定义，没有分片校验和信息。
// 一般仅在http简单下载场合使用。
// 之后应当调用IndexRest构建剩余分片索引信息。
//
// 	@size 文件大小，应大于零。
// 	@bsz  分片大小，应大于零。零值引发panic
//
func Divide(size, bsz int64) (map[int64]*Piece, error) {
	if size <= 0 {
		return nil, errSize
	}
	if bsz <= 0 {
		return panic("block size invalid")
	}
	buf := make(map[int64]*Piece)
	cnt, mod := size/bsz, size%bsz

	var i int64
	for i < cnt {
		off := i * bsz
		buf[off] = &Piece{off, off + bsz, nil}
		i++
	}
	if mod > 0 {
		off := cnt * bsz
		buf[off] = &Piece{off, off + mod, nil}
	}
	return buf, nil
}

//
// PieceSumor 分片校验和生成器。
// 按分片定义读取目标数据，计算校验和并设置。
//
type PieceSumor struct {
	r    io.ReaderAt
	data map[int64]*Piece
	ch   chan interface{}
}

//
// NewPieceSumor 新建一个分片校验和生成器。
// list清单为针对r的源文件大小分割而来。
// 分片校验和设置在list成员的Sum字段上。
//
// 返回值仅可单次使用，由 Do|FullDo 实施。
// list是一个引用，外部不应该再修改它。
//
func NewPieceSumor(r io.ReaderAt, list map[int64]*Piece) *PieceSumor {
	bs := PieceSumor{
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
// Task 获取一个分片定义。
//
func (bs *PieceSumor) Task() interface{} {
	return <-bs.ch
}

//
// Work 计算一个分片数据的校验和。
//
func (bs *PieceSumor) Work(k interface{}) error {
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
// Do 计算/设置分片校验和（有限并发）。
//
// 启动 limit 个并发计算，传递0采用内置默认值（SumThread）。
// 返回的等待对象用于等待全部工作完成。
//
// 应用需要处理bad内的值，并且需要清空。
//
func (bs *PieceSumor) Do(limit int, bad chan<- error, cancel func() bool) *sync.WaitGroup {
	if limit <= 0 {
		limit = SumThread
	}
	return goes.Works(goes.LimitTasker(bs, limit), bad, cancel)
}

//
// FullDo 计算/设置分片校验和（完全并发）。
//
// 有多少个分片启动多少个协程。其它说明与Do相同。
// 注：通常仅在分片较大数量较少时采用。
//
func (bs *PieceSumor) FullDo(bad chan<- error, cancel func() bool) *sync.WaitGroup {
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
