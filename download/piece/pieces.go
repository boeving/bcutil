// Package piece 数据分片。
package piece

///////////////////
/// 分片&校验规则
/// 1.
/// 分片校验集文件。
/// 格式：8+[20]+[20][20]...
/// 	- 前8字节定义规范版本、分片大小和分片数量。
/// 		[8]  版本号。默认0，遵循默认规则（如下）。
/// 		[24] 分片大小，最大支持到16MB。
/// 		[32] 分片数量，最大支持到4G。
/// 	- 随后20字节为根哈希（后面哈希序列的单层汇总）。
/// 	- 再后面为每单元20字节的各分片哈希校验和（sha1）序列。
/// 2.
/// 多级分片。
/// 分片校验集文件如果较大，可作为普通文件再分片和校验定义。
/// 通常，2级分片已经足够应对超大型文件。
///
/// 假设分片大小为16MB，分片数量为4G，则：
/// 	- 最大可支持64PB单文件的校验定义（16MB * 4G）。
/// 	- 此时1级校验集文件为80GB，2级分片校验集文件最小可至100kb（80000/16 * 20）。
/// 	  假设2级分片大小设计为1MB，则其分片校验集文件为1.6MB。
/// 3.
/// 单层校验根。
/// 各分片哈希校验和按顺序串接后计算根哈希，不采用默克尔树。
/// 理由：
/// - 因为2级分片已把分片校验集数据缩小到足够小，已便于获取。
/// - 下载的分片数据计算校验和后，与目标值做简单的相等比较即可，无需循树计算根匹配。
/// - 在现代高速网络或archives公共服务的环境下，数据获取成本降低了。
///
/// 单层根哈希也称为整集哈希。
/// 	整集哈希 = Sha1(分片校验和...)
/// 注：
/// 对文件本身数据直接的校验和计算输出为文件摘要或文件哈希。
/// 	文件哈希 = Sha1(文件)
///
///////////////////////////////////////////////////////////////////////////////

import (
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/qchen-zh/pputil/goes"
)

// 基本常量
const (
	// 校验和哈希长度（sha1）
	// 通常由文件或整集哈希定位后，再询问分片，故长度已足够。
	SumLength = 20
	// 分片单位值（8k）
	PieceUnit = 1 << 13

	// 分片校验和构造默认并发量
	// 行为：读取分片数据，Hash计算
	DefaultSumThread = 8
	// 默认分片大小
	// 8k*32 = 256k
	DefaultPieceSize = PieceUnit * 32
)

var (
	errChkSum = errors.New("checksum not match")
	errZero   = errors.New("read end offset is zero")
)

//
// Error 分片错误。
//
type Error struct {
	Off int64
	Err error
}

func (p Error) Error() string {
	return fmt.Sprintf("[%d] %v", p.Off, p.Err)
}

// HashSum 哈希校验和。
type HashSum [SumLength]byte

//
// Pieces 分片定义集。
// 定义分片数据的基本属性：版本、分片大小、分片数量和哈希校验和序列。
// 这些属性会被存储于外部，用于文件分享传递的依据。
//
// 存储头部4或8字节为分片大小与数量定义，
// 然后是各分片的哈希校验和（连续串接，无间隔字节）。
//
// 头部4字节中：前20位定义分片数量，后12位定义分片大小。
//  - 分片单位8k（PieceUnit）；
//  - 分片最大支持到32MB（8k*4095）。
//  - 0分片大小表示不分片；
//  - PieceUnit
//
type Pieces struct {
	Amount int   // 分片数量
	Span   int64 // 分片大小（bytes）

	// 校验集（key: offset），
	// 可能从文件中读取已经存在的分片定义。
	// 或外部计算校验和后（Sumor）赋值，然后可用于构造存储。
	Sums map[int64]HashSum
}

//
// OnePieces 单分片定义。
// 设置为单一分片的值（也即未分片），常用于对种子文件的直接下载。
// sum 即为文件本身的哈希。
//
func OnePieces(sum HashSum) *Pieces {
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

	// 后12bit为分片大小（8k单位）
	p.Span = int64((n2<<20)>>20) * PieceUnit

	return nil
}

//
// Load 读取分片定义。
// 分片定义为20字节哈希连续存储。
// 结构：4|8+[20][20]...
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
// 结构：4|8+[20][20]...
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
// Indexes 返回分片定义集的下标索引集。
// 返回值可能是一个0长度的分片（如果为零分片的话）。
//
func (p *Pieces) Indexes() []int64 {
	buf := make([]int64, 0, len(p.Sums))

	for k := range p.Sums {
		buf = append(buf, k)
	}
	return buf
}

//
// Empty 分片集是否为空。
//
func (p *Pieces) Empty() bool {
	return len(p.Sums) == 0
}

//
// RestPieces 剩余待下载分片。
// 存储结构与Pieces类似，但包含下标偏移。
// 注：因分片不连续。
//
// 仅用于下载过程中的状态存储，下载完毕即无用。
// 如果存在一个文件对应的剩余分片定义，则表示未下载完毕。
//
type RestPieces struct {
	Pieces
}

const lenOffSum = 8 + SumLength

//
// Load 读取未下载索引数据。
// 结构：4|8+[8+20][8+20]...
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
// Bytes 编码剩余分片索引集。
// 结构：4|8+[8+20][8+20]...
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
func (s *Sumor) Work(k interface{}, _ *goes.Sema) error {
	p := k.(Piece)
	data, err := blockRead(s.ra, p.Begin, p.End)

	if err != nil {
		return err
	}
	s.vch <- OffSum{
		p.Begin, sha1.Sum(data)}

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
func (sc *SumChecker) Work(k interface{}, _ *goes.Sema) error {
	os := k.(OffSum)
	data, err := blockRead(sc.RA, os.Off, os.Off+sc.Span)

	if err != nil {
		return Error{os.Off, err}
	}
	if sha1.Sum(data) != os.Sum {
		return Error{os.Off, errChkSum}
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
// SumAll 输入数据校验核实。
//
// 内部即为对SumChecker的使用，并发工作（并发量采用默认值）。
// span 为分块大小（bytes）。
//
func SumAll(ra io.ReaderAt, span int64, list map[int64]HashSum) bool {
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
		return nil, Error{begin, err}
	}
	return buf, nil
}
