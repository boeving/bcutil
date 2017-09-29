// Package download 下载器。
// 支持并发、断点续传。
// 外部实现特定的下载方式，如http直接下载或P2P传输。
// 由用户定义下载响应集。
//
package download

import (
	"errors"
	"fmt"
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
// Hauler 数据搬运工。
// 实施单个目标（分片）的具体下载行为，
//
type Hauler interface {
	// 下载分片。
	Start(Piece) error

	// 注册完成回调。
	Completed(func(Piece, []byte))

	// 注册失败回调。
	Failed(func(Piece, error))
}

//
// Downloader 下载器。
// 负责目标数据的分片，组合校验，缓存等。
//
type Downloader struct {
	Monitor                // 监控器
	Rest    PieceRest      // 未下载分片信息集
	Cacher  io.WriteSeeker // 分片数据缓存
}

//
// IndexInit 分片索引初始化。
// 构建待下载分片索引及其关联数据。
//
// 如果r值不为nil，从外部输入读取，否则从List成员构建。
// 从r输入构建时无需预先赋值List成员值。
//
func (m *Downloader) IndexInit(r io.Reader) error {
	if m.Index == nil {
		return
	}
	buf := make(map[int64]struct{}, len(m.List))

	for off := range m.List {
		buf[off] = struct{}{}
	}
	m.restIndexes = buf
}

//
// LoadIndexes 载入待下载分片索引。
//
// 后续续传时需要，List成员无需初始赋值。
// 需要读取Indexer成员，应当已经赋值。
//
// 之后应当调用IndexRest构建剩余分片索引信息。
//
func (m *Downloader) LoadIndexes() error {
	if m.List == nil {
		m.List = make(map[int64]*Piece)
	}
	var buf [lenPieceIdx]byte
	for {
		if _, err := io.ReadFull(m.Indexer, buf[:]); err != nil {
			return fmt.Errorf("cache index invalid: %v", err)
		}
		var b Piece
		b.Read(buf)
		m.List[b.Begin] = &b
	}
	return nil
}

//
// 载入剩余分片索引。
// 输入结构：
//
//
func restIndexes(list map[int64]*Piece, r io.Reader) error {
	var buf [lenPieceIdx]byte
	for {
		if _, err := io.ReadFull(m.Indexer, buf[:]); err != nil {
			return fmt.Errorf("cache index invalid: %v", err)
		}
		var b Piece
		b.Read(buf)
		m.List[b.Begin] = &b
	}

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
func (m *Downloader) Divide(bsz int64) error {
	buf, err := Divide(m.Size, bsz)
	if err != nil {
		return err
	}
	m.List = buf
	return nil
}

//
// PieceGetter 获取分片定义取值渠道。
//
func (m *Downloader) PieceGetter() <-chan Piece {
	ch := make(chan Piece, 1)

	go func() {
		for _, v := range m.List {
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
func (m *Downloader) SaveCache(bs []PieceData) (num int, err error) {
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
// SaveIndexs 缓存剩余分片索引。
//
func (m *Downloader) SaveIndexs() (int, error) {
	if len(m.restIndexes) == 0 {
		return 0, nil
	}
	n := 0
	buf := make([]byte, 0, len(m.restIndexes)*lenPieceIdx)

	for off := range m.restIndexes {
		b := m.List[off]
		if b == nil {
			return 0, errIndex
		}
		buf = append(buf, b.Bytes()...)
	}

	return m.Indexer.Write(buf)
}
