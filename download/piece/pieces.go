// Package piece 数据分片。
package piece

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
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
	errChkSum = errors.New("checksum not match")
	errZero   = errors.New("read end offset is zero")
)

//
// PieError 分片错误。
//
type PieError struct {
	Off int64
	Err error
}

func (p PieError) Error() string {
	return fmt.Sprintf("[%d] %v", p.Off, p.Err)
}

// HashSum 哈希校验和。
type HashSum [SumLength]byte

//
// Pieces 分片集。
// 头部4字节存储分片数量和分片大小（16k单位）。
// 其中前20位定义分片数量，后12位定义分片大小。
//  - 最小分片16k（16k*1）；
//  - 最大分片约64MB（16k*4096）。
//  - 0分片大小表示不分片；
// 友好：
// 小于16k的文本文档无需分片，简化逻辑。
//
type Pieces struct {
	Amount int               // 分片数量
	Span   int64             // 分片大小（bytes）
	Sums   map[int64]HashSum // 校验集（key: offset）
}

//
// NewPieces 新建一个分片集。
// 设置为单一分片的默认值（也即未分片）。
// 常用于对种子文件的直接下载。
//
// 通常情况下，外部是直接声明一个实例，
// 然后调用Head和Load从种子文件中载入配置。
//
func NewPieces(sum HashSum) *Pieces {
	return &Pieces{
		1, 0,
		map[int64]HashSum{0: sum},
	}
}

//
// Head 读取头部分片大小定义。
// 外部需要保证读取流游标处于头部位置。
//
func (p *Pieces) Head(r io.Reader) error {
	var b [4]byte
	if _, err := r.Read(b[:]); err != nil {
		return err
	}
	n2 := binary.BigEndian.Uint32(b[:])

	// 前20bit为分片总数
	p.Amount = int(n2 >> 12)

	// 后12bit为分片大小（16k单位）
	p.Span = int64((n2<<20)>>20) * PieceUnit

	return nil
}

//
// Load 读取分片定义。
// 分片定义为32字节哈希连续存储。
// 结构：4+[32][32]...
//
// 0值表示无分片，应仅读取一个哈希序列。
// 正常返回的error值为io.EOF。
//
// 外部应保证读取流处于正确的位置（Head调用之后）。
//
func (p *Pieces) Load(r io.Reader) error {
	if p.Sums == nil {
		p.Sums = make(map[int64]HashSum)
	}
	for i := 0; i < p.Amount; i++ {
		var sum HashSum
		if _, err := io.ReadFull(r, sum[:]); err != nil {
			return err
		}
		p.Sums[int64(i)*p.Span] = sum

		if p.Span == 0 {
			break
		}
	}
	return nil
}

//
// Bytes 编码分片集整体字节序列。
// 结构：4+[32][32]...
//
func (p *Pieces) Bytes() []byte {
	buf := make([]byte, 0, 4+len(p.Sums)*SumLength)

	// Head[4]...
	buf = append(buf, p.HeadBytes()...)

	for _, sum := range p.Sums {
		buf = append(buf, sum[:]...)
	}
	return buf
}

//
// HeadBytes 构造头部4字节序列。
//
func (p *Pieces) HeadBytes() []byte {
	var buf [4]byte

	n2 := uint32(p.Amount) << 12
	n2 = n2 | uint32(p.Span/PieceUnit)&0xfff

	binary.BigEndian.PutUint32(buf[:], n2)

	return buf[:]
}

//
// Indexes 下标索引集。
//
func (p *Pieces) Indexes() []int64 {
	buf := make([]int64, 0, len(p.Sums))

	for k := range p.Sums {
		buf = append(buf, k)
	}
	return buf
}

//
// Empty 分片集是否为空
//
func (p *Pieces) Empty() bool {
	return len(p.Sums) == 0
}

//
// RestPieces 剩余分片。
// 存储结构与Pieces有区别（含下标偏移）
// 注：因分片已不连续。
//
type RestPieces struct {
	Pieces
}

const lenOffSum = 8 + SumLength

//
// Load 读取未下载索引数据。
// 结构：4+[8+32][8+32]...
//
func (p *RestPieces) Load(r io.Reader) error {
	if p.Sums == nil {
		p.Sums = make(map[int64]HashSum)
	}
	// 按头部定义次数循环
	for i := 0; i < p.Amount; i++ {
		var buf [lenOffSum]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return err
		}
		off, sum := offsetAndSum(buf)
		p.Sums[int64(off)] = *sum
	}
	return nil
}

func offsetAndSum(buf [lenOffSum]byte) (uint64, *HashSum) {
	var sum HashSum
	copy(sum[:], buf[8:])

	return binary.BigEndian.Uint64(buf[0:]), &sum
}

//
// Bytes 编码剩余分片索引集（整体）。
// 包含位置下标偏移值，结构：4+[8+32][8+32]...
//
func (p *RestPieces) Bytes() []byte {
	buf := make([]byte, 0, 4+len(p.Sums)*lenOffSum)
	buf = append(buf, p.HeadBytes()...)

	off := make([]byte, 8)
	for k, sum := range p.Sums {
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

// Size 分片大小。
func (p Piece) Size() int {
	return int(p.End - p.Begin)
}

//
// Divide 分片配置服务。
// 对确定的数据大小进行分片定义。
// 	fsize 文件大小，负值或零无效。
// 	span  分片大小，负值或零或大于fsize，取等于fsize。
//
func Divide(fsize, span int64) <-chan Piece {
	if fsize < 0 {
		return nil
	}
	if span <= 0 || span > fsize {
		span = fsize
	}
	cnt, mod := fsize/span, fsize%span
	ch := make(chan Piece)

	go func() {
		var i int64
		for i < cnt {
			off := i * span
			ch <- Piece{off, off + span}
			i++
		}
		if mod > 0 {
			off := cnt * span
			ch <- Piece{off, off + mod}
		}
		close(ch)
	}()
	return ch
}

//
// OffSum 偏移&校验和值对。
// 用于 map[int64]HashSum 并发数据的传递。
//
type OffSum struct {
	Off int64
	Sum HashSum
}

//
// Sumor 校验和生成器。
// 由 Do|FullDo 实施，仅可单次使用。
//
type Sumor struct {
	ra   io.ReaderAt
	list map[int64]HashSum
	ch   <-chan Piece
	vch  chan OffSum
	sem  chan struct{} // 取值锁
}

//
// NewSumor 新建一个分片校验和生成器。
//  ra 输入流需要支持Seek（Seeker接口）。
//  span 传递0或负值采用默认分片大小。
//
func NewSumor(ra io.ReaderAt, fsize, span int64) *Sumor {
	if span <= 0 {
		span = DefaultPieceSize
	}
	sm := Sumor{
		ra:   ra,
		list: make(map[int64]HashSum),
		sem:  make(chan struct{}),
		// 一个分片服务
		ch:  Divide(fsize, span),
		vch: make(chan OffSum),
	}

	// 单独的赋值服务（list非并发安全）
	go func() {
		for v := range sm.vch {
			sm.list[v.Off] = v.Sum
		}
		close(sm.sem)
	}()

	return &sm
}

//
// List 返回校验和清单。
// 需要在 Do|FullDo 成功构建之后调用。
//
func (s *Sumor) List() map[int64]HashSum {
	<-s.sem
	return s.list
}

//
// Task 获取一个分片定义。
//
func (s *Sumor) Task() (k interface{}, ok bool) {
	k, ok = <-s.ch
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
	s.vch <- OffSum{
		p.Begin, sha256.Sum256(data)}

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
// SumChecker 校验和检查器。
// 用于完整文件依据校验清单整体核实。
// （内部并发检查）
//
type SumChecker struct {
	RA   io.ReaderAt
	Span int64
	List map[int64]HashSum
	ch   <-chan OffSum
}

//
// NewSumChecker 创建一个校验和检查器。
// 注：只能使用一次。
//
func NewSumChecker(ra io.ReaderAt, span int64, list map[int64]HashSum) *SumChecker {
	ch := make(chan OffSum)
	sc := SumChecker{
		RA:   ra,
		Span: span,
		List: list,
		ch:   ch,
	}
	go func() {
		for k, sum := range sc.List {
			// no sc.ch (only read)
			ch <- OffSum{k, sum}
		}
		close(ch)
	}()
	return &sc
}

//
// Task 返回偏移下标。
//
func (sc *SumChecker) Task() (k interface{}, ok bool) {
	k, ok = <-sc.ch
	return
}

//
// Work 读取数据核实校验和。
// 不符合则返回一个错误。
//
func (sc *SumChecker) Work(k interface{}) error {
	os := k.(OffSum)
	data, err := blockRead(sc.RA, os.Off, os.Off+sc.Span)

	if err != nil {
		return PieError{os.Off, err}
	}
	if sha256.Sum256(data) != os.Sum {
		return PieError{os.Off, errChkSum}
	}
	return nil
}

//
// Check 计算校验。
// 简单计算，有任何一片不符即返回。
//
func (sc *SumChecker) Check(limit int) <-chan error {
	if limit <= 0 {
		limit = DefaultSumThread
	}
	return goes.Works(goes.LimitTasker(sc, limit))
}

//
// CheckAll 校验和完整计算对比。
// 返回的错误PieError可用于收集不合格的分片。
//
func (sc *SumChecker) CheckAll(limit int) <-chan error {
	if limit <= 0 {
		limit = DefaultSumThread
	}
	return goes.WorksLong(goes.LimitTasker(sc, limit), nil)
}

//
// CheckSum 输入数据校验核实。
//
// 内部即为对SumChecker的使用，并发工作（并发量采用默认值）。
// span 为分块大小（bytes）。
//
func CheckSum(ra io.ReaderAt, span int64, list map[int64]HashSum) bool {
	ech := NewSumChecker(ra, span, list).Check(0)
	return <-ech == nil
}

//
// Ordered 返回按偏移排序后的哈希序列。
// 已知分片大小的优化版。
//
// list需为完整的清单，如果下标偏移超出范围，返回nil。
// 主要用于默克尔树及树根的计算。
//
func Ordered(span int64, list map[int64]HashSum) []HashSum {
	buf := make([]HashSum, len(list))

	for off, sum := range list {
		i := int(off / span)
		if i >= len(buf) {
			return nil
		}
		buf[i] = sum
	}
	return buf
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
		return nil, PieError{begin, err}
	}
	return buf, nil
}
