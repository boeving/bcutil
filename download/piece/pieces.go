// Package piece 数据分片。
package piece

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"sync"

	"github.com/qchen-zh/pputil/goes"
)

// 基本常量
const (
	SumLength = 32      // 校验和哈希长度（sha256）
	PieceUnit = 1 << 14 // 分片单位值（16k）

	// 分片构造默认并发量
	// 行为：读取分片数据，Hash计算
	DefaultSumThread = 12
	// 默认分片大小
	// 16k*16 = 256k
	DefaultPieceSize = PieceUnit * 16
)

var (
	errSize    = errors.New("file size invalid")
	errWriteAt = errors.New("the Writer not support WriteAt")
	errIndex   = errors.New("the indexs not in blocks")
)

// HashSum sha1校验和。
type HashSum [SumLength]byte

//
// Pieces 分片集。
// 头部1字节存储分片大小（16k单位）。
//  - 0分片大小表示不分片；
//  - 最小分片16k（16k*1）；
//  - 最大分片约4MB大小（16k*255）。
// 友好：
// 小于16k的文本文档无需分片，简化逻辑。
//
type Pieces struct {
	Span  int64              // 分片大小（bytes）
	Index map[int64]*HashSum // 校验集（key: offset）
}

//
// NewPieces 创建一个分片集对象。
//
func NewPieces(size int64) Pieces {
	return Pieces{
		Span:  size,
		Index: make(map[int64]*HashSum),
	}
}

//
// Head 读取头部分片大小定义。
//
func (p *Pieces) Head(r io.ReadSeeker) (err error) {
	if _, err = r.Seek(0, 0); err != nil {
		return
	}
	var b [1]byte
	if _, err = r.Read(b[:]); err != nil {
		return
	}
	p.Span = int64(b[0]) * PieceUnit
}

//
// Read 读取分片定义。
// 分片定义为32字节哈希连续存储。
//  结构：1+[32][32]...
//
// 0值表示无分片，应仅包含一个哈希序列。
//
func (p *Pieces) Read(r io.ReadSeeker) (err error) {
	if _, err = r.Seek(1, 0); err != nil {
		return
	}
	i := 0
	for {
		var buf [SumLength]byte
		if _, err = io.ReadFull(r, buf[:]); err != nil {
			break
		}
		p.Index[i*p.Span] = &buf
		i++
		if p.Span == 0 {
			break
		}
	}
	return
}

//
// Bytes 编码分片集。
// 结构：1+[32][32]...
//
func (p *Pieces) Bytes() []byte {
	buf := make(1 + len(p.Index)*SumLength)
	buf[1] = byte(p.Span / PieceUnit)

	for _, h := range p.Index {
		buf = append(buf, (*h)[:]...)
	}
	return buf
}

//
// RestIndex 剩余分片索引。
// 存储结构与Pieces稍有差别（包含下标偏移值）
//  结构：1+[8+32][8+32]...
//
type RestIndex struct {
	Pieces
}

const lenOffSum = 8 + SumLength

//
// Read 读取未下载索引数据。
// 结构：1+[8+32][8+32]...
//
func (p *PieceRest) Read(r io.ReadSeeker) (err error) {
	if _, err = r.Seek(1, 0); err != nil {
		return
	}
	for {
		var buf [lenOffSum]byte
		if _, err = io.ReadFull(r, buf[:]); err != nil {
			break
		}
		off, hash := offsetHash(buf)
		p.Index[int64(off)] = hash
	}
	return
}

func offsetHash(buf *[lenOffSum]byte) (uint64, *HashSum) {
	var tmp [SumLength]byte
	copy(tmp[:], buf[8:])

	return binary.BigEndian.Uint64(buf[0:]), &tmp
}

//
// Bytes 编码剩余分片索引集。
// 结构：1+[8+32][8+32]...
//
func (p *PieceRest) Bytes() []byte {
	buf := make(1 + len(p.Index)*lenOffSum)
	buf[1] = byte(p.Span / PieceUnit)
	off := make([]byte, 8)

	for k, h := range p.Index {
		binary.BigEndian.PutUint64(off, uint64(k))
		buf = append(buf, off...)
		buf = append(buf, (*h)[:]...)
	}
	return buf
}

//
// Data 分片数据
// 注：用于存储。
//
type Data struct {
	Offset int64
	Data   []byte
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
// Sumor 分片校验和生成器。
// 按分片定义读取目标数据，计算校验和并设置。
//
type Sumor struct {
	r    io.ReaderAt
	data map[int64]*Piece
	ch   chan interface{}
}

//
// Sumor 新建一个分片校验和生成器。
// list清单为针对r的源文件大小分割而来。
// 分片校验和设置在list成员的Sum字段上。
//
// 返回值仅可单次使用，由 Do|FullDo 实施。
// list是一个引用，外部不应该再修改它。
//
func Sumor(r io.ReaderAt, list map[int64]*Piece) *Sumor {
	bs := Sumor{
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
func (bs *Sumor) Task() interface{} {
	return <-bs.ch
}

//
// Work 计算一个分片数据的校验和。
//
func (bs *Sumor) Work(k interface{}) error {
	b := bs.data[k.(int64)]
	if b == nil {
		return errIndex
	}
	data, err := blockRead(bs.r, b.Begin, b.End)

	if err != nil {
		return err
	}
	b.Sum = new(HashSum)
	copy((*b.Sum)[:], sha256.Sum256(buf))

	return nil
}

//
// Do 计算/设置分片校验和（有限并发）。
//
// 启动 limit 个并发计算，传递0采用内置默认值（DefaultSumThread）。
// 返回的等待对象用于等待全部工作完成。
//
// 应用需要处理bad内的值，并且需要清空。
//
func (bs *Sumor) Do(limit int, bad chan<- error, cancel func() bool) *sync.WaitGroup {
	if limit <= 0 {
		limit = DefaultSumThread
	}
	return goes.Works(goes.LimitTasker(bs, limit), bad, cancel)
}

//
// FullDo 计算/设置分片校验和（完全并发）。
//
// 有多少个分片启动多少个协程。其它说明与Do相同。
// 注：通常仅在分片较大数量较少时采用。
//
func (bs *Sumor) FullDo(bad chan<- error, cancel func() bool) *sync.WaitGroup {
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
// CheckSum 输入数据校验。
//
func CheckSum(r io.Reader, cksum HashSum) bool {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return false
	}
	if h.Sum(nil) != cksum {
		return false
	}
	return true
}
