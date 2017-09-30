// Package piece 数据分片。
package piece

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"

	"github.com/qchen-zh/pputil/goes"
)

// 基本常量
const (
	SumLength = 32      // 校验和哈希长度（sha256）
	PieceUnit = 1 << 14 // 分片单位值（16k）

	// 分片校验和构造默认并发量
	// 行为：读取分片数据，Hash计算
	DefaultSumThread = 8
	// 默认分片大小
	// 16k*16 = 256k
	DefaultPieceSize = PieceUnit * 16
)

var (
	// 读取末端为起始0下标。
	errZero     = errors.New("read end offset is zero")
	errChecksum = errors.New("checksum was failed")
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
	Span  int64             // 分片大小（bytes）
	Index map[int64]HashSum // 校验集（key: offset）
}

//
// NewPieces 创建一个分片集对象。
// @size 为分片大小（bytes）。
//
func NewPieces(size int64) Pieces {
	return Pieces{
		Span:  size,
		Index: make(map[int64]HashSum),
	}
}

//
// Head 读取头部分片大小定义。
// 外部需要保证读取流游标处于头部位置。
//
func (p *Pieces) Head(r io.Reader) error {
	var b [1]byte
	if _, err := r.Read(b[:]); err != nil {
		return err
	}
	p.Span = int64(b[0]) * PieceUnit
	return nil
}

//
// Read 读取分片定义。
// 分片定义为32字节哈希连续存储。
//  结构：1+[32][32]...
//
// 0值表示无分片，应仅包含一个哈希序列。
//
func (p *Pieces) Read(r io.Reader) error {
	i := 0
	for {
		var sum [SumLength]byte
		if _, err := io.ReadFull(r, sum[:]); err != nil {
			return err
		}
		p.Index[i*p.Span] = sum

		if p.Span == 0 {
			break
		}
		i++
	}
	return nil
}

//
// Bytes 编码分片集。
// 结构：1+[32][32]...
//
func (p *Pieces) Bytes() []byte {
	buf := make([]byte, 1, 1+len(p.Index)*SumLength)
	buf[1] = byte(p.Span / PieceUnit)

	for _, sum := range p.Index {
		buf = append(buf, sum[:]...)
	}
	return buf
}

//
// RestPieces 剩余分片。
// 存储结构与Pieces稍有差别（包含下标偏移值）
//  结构：1+[8+32][8+32]...
//
type RestPieces struct {
	Pieces
}

const lenOffSum = 8 + SumLength

//
// Read 读取未下载索引数据。
// 结构：1+[8+32][8+32]...
//
func (p *RestPieces) Read(r io.Reader) error {
	for {
		var buf [lenOffSum]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return err
		}
		off, sum := offsetAndSum(buf)
		p.Index[int64(off)] = *sum
	}
	return nil
}

func offsetAndSum(buf *[lenOffSum]byte) (uint64, *HashSum) {
	var sum [SumLength]byte
	copy(sum[:], buf[8:])

	return binary.BigEndian.Uint64(buf[0:]), &sum
}

//
// Bytes 编码剩余分片索引集。
// 结构：1+[8+32][8+32]...
//
func (p *RestPieces) Bytes() []byte {
	buf := make([]byte, 1, 1+len(p.Index)*lenOffSum)
	buf[1] = byte(p.Span / PieceUnit)
	off := make([]byte, 8)

	for k, sum := range p.Index {
		binary.BigEndian.PutUint64(off, uint64(k))
		buf = append(buf, off...)
		buf = append(buf, sum[:]...)
	}
	return buf
}

//
// Piece 分片定义。
//
type Piece struct {
	Begin int64
	End   int64
}

//
// Divide 分片配置服务。
// 对确定的数据大小进行分片定义。
// 	fsize 文件大小，负值与零相同。
// 	psz   分片大小，负值或零或大于fsize，取等于fsize。
//
func Divide(fsize, psz int64) <-chan Piece {
	if fsize < 0 {
		fsize = 0
	}
	if psz <= 0 || psz > fsize {
		psz = fsize
	}
	cnt, mod := fsize/psz, fsize%psz
	ch := make(chan Piece)

	go func() {
		i := 0
		for i < cnt {
			off := i * psz
			ch <- Piece{off, off + psz}
			i++
		}
		if mod > 0 {
			off := cnt * psz
			ch <- Piece{off, off + mod}
		}
		close(ch)
	}()
	return ch
}

//
// Sumor 校验和生成器。
// 由 Do|FullDo 实施，仅可单次使用。
//
type Sumor struct {
	ra   io.ReaderAt
	ch   <-chan Piece
	list map[int64]HashSum
}

//
// NewSumor 新建一个分片校验和生成器。
//  ra  输入流需要支持Seek（Seeker接口）。
//  psz 传递0或负值采用默认分片大小。
//
func NewSumor(ra io.ReaderAt, fsize, psz int64) *Sumor {
	if psz <= 0 {
		psz = DefaultPieceSize
	}
	sm := Sumor{
		ra:   ra,
		ch:   Divide(fsize, psz), // 分片服务
		list: make(map[int64]HashSum),
	}
	return &sm
}

//
// List 返回校验和清单。
// 需要在 Do|FullDo 成功构建之后调用。
//
func (s *Sumor) List() map[int64]HashSum {
	return s.list
}

//
// Task 获取一个分片定义。
//
func (s *Sumor) Task() (v interface{}, ok bool) {
	v, ok = <-s.ch
	return
}

//
// Work 计算一个分片数据的校验和。
//
func (s *Sumor) Work(k interface{}) error {
	p := k.(Piece)
	data, err := blockRead(s.ra, p.Begin, p.End)

	if err != nil {
		return err
	}
	s.list[p.Begin] = sha256.Sum256(data)

	return nil
}

//
// Do 计算/设置分片校验和（有限并发）。
//
// 启动 limit 个并发计算，传递0采用内置默认值（DefaultSumThread）。
// 返回的等待对象用于等待全部工作完成。
//
func (s *Sumor) Do(limit int) <-chan error {
	if limit <= 0 {
		limit = DefaultSumThread
	}
	return goes.Works(goes.LimitTasker(s, limit))
}

//
// FullDo 计算/设置分片校验和。
//
// 无限并发，有多少个分片启动多少个协程。其它说明与Do相同。
// 注：通常仅在分片较大数量较少时采用。
//
func (s *Sumor) FullDo() <-chan error {
	return goes.Works(s)
}

//
// 读取特定片区的数据。
// 读取出错后返回错误（通知外部）。
//
func blockRead(r io.ReaderAt, begin, end int64) ([]byte, error) {
	if end == 0 {
		return nil, errZero
	}
	buf := make([]byte, begin-end)
	n, err := r.ReadAt(buf, begin)

	if n < len(buf) {
		return nil, err
	}
	return buf, nil
}

//
// SumChecker 校验和检查器。
// 用于完整文件依据校验清单整体核实。
//
type SumChecker struct {
	RA   io.ReaderAt
	Span int64
	List map[int64]HashSum
	ch   <-chan int64
}

//
// NewSumChecker 创建一个校验和检查器。
// 注：只能使用一次。
//
func NewSumChecker(ra io.ReaderAt, span int64, list map[int64]HashSum) *SumChecker {
	ch := make(chan int64)
	sc := SumChecker{
		RA:   ra,
		Span: span,
		List: list,
		ch:   ch,
	}
	go func() {
		for k := range sc.List {
			ch <- k // no sc.ch
		}
		close(ch)
	}()
	return &sc
}

//
// Task 返回偏移下标。
//
func (sc *SumChecker) Task() (v interface{}, ok bool) {
	v, ok = sc.ch
	return
}

//
// Work 读取数据核实校验和。
// 不符合则返回一个错误。
//
func (sc *SumChecker) Work(k interface{}) error {
	off := k.(int64)
	data, err := blockRead(sc.RA, off, off+sc.Span)

	if err != nil {
		return err
	}
	if sha256.Sum256(data) != sc.List[off] {
		return errChecksum
	}
	return nil
}

//
// Do 校验和计算对比。
//
func (sc *SumChecker) Do(limit int) <-chan error {
	if limit <= 0 {
		limit = DefaultSumThread
	}
	return goes.Works(goes.LimitTasker(sc, limit))
}

//
// CheckSum 输入数据校验核实。
//
// 内部即为对SumChecker的使用，并发工作（并发量采用默认值）。
// span 为分块大小（bytes）。
//
func CheckSum(ra io.ReaderAt, span int64, list map[int64]HashSum) bool {
	ech := NewSumChecker(ra, span, list).Do(0)
	return <-ech == nil
}

//
// Ordered 返回按偏移排序后的哈希序列。
// 已知分片大小的优化版。
//
// 主要用于默克尔树及树根的计算。
//
func Ordered(span int64, list map[int64]HashSum) []HashSum {
	buf := make([]HashSum, len(list))

	for off, sum := range list {
		buf[off/span] = sum
	}
	return buf
}
