// Package piece 数据分片包。
// 定义数据的分片和校验规则。
//
// 文件在存储和传输中，通常需要一个正确性验证。
// 通常的验证为计算一个文件的校验和，或在网络分片传输中，构造一个默克尔树。
//
// 本分片包不采用传统的默克尔树，而仅是采用平面的哈希表，根哈希为哈希表的串连汇总计算。
// 这是一种简化方式，但同时也因此便于支持超大型文件的分片传输和校验。
// 客户端不必在内存中构造一个大型的默克尔树，分片的校验和可以从磁盘读取后直接比较验证。
//
// 1. 校验集文件。
//  格式：8+[32] + [32][32]...
//  - 前8字节定义版本、分片大小和分片数量。
//  	[8]  版本号。默认0，遵循默认规则（如下）。
//  	[24] 分片大小，最大支持到16MB。
//  	[32] 分片数量，最大支持到4G。
//  - 随后32字节为分片校验和序列的根哈希（又名整集哈希，串连合并计算）。
//  - 再后面为每单元32字节的各分片校验和序列。
//
// 2. 多级分片。
//
// 对于大型文件，校验集文件本身会很大，因此作为普通文件再做分片。
// 通常，2级分片已经足够。
//
// 假设都取最大值，分片大小为16MB，分片数量为4G，则最大文件可支持到64PB（16MB * 4G）。
// 此时1级校验集文件为128GB，如果2级分片大小定义为1MB，则分片校验集文件为4MB（128000/1000 * 32）。
// 校验集文件大小已可接受。
//
// 3. 校验算法：sha256.Sum256(...)
//
// 注：
// 对文件本身直接的校验和计算称为文件哈希，算法同上。
//
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
	// 分片校验和构造默认并发量
	// 行为：读取分片数据，Hash计算
	SumThread = 8

	// 默认分片大小（256kb）
	PieceSize = 1 << 18
)

const (
	lenHead   = 8 + 32    // 头部长度
	lenSum    = 32        // 哈希长度（bytes）
	lenRest   = 8 + 32    // 剩余分片存储单元长度
	maxAmount = 1<<32 - 1 // 最多分片数量（4字节）
	maxSpan   = 1<<24 - 1 // 最大分片大小（3字节）
)

var (
	errChkSum = errors.New("checksum not match")
	errZero   = errors.New("no data for reading")
	errExit   = errors.New("stop by external caller")
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

//
// Output 输出分片校验集。
// 返回输出的字节数和可能的错误。
//
func Output(w io.WriterAt, h *Head, ss *Sums) (int, error) {
	w.WriteAt(h.Bytes(), 0)
	n, err := ss.Puts(w)
	return n + h.Size(), err
}

// Hash32 32字节哈希校验和。
type Hash32 [lenSum]byte

//
// Head 分片头部定义。
// 结构：8 + 32
// 分片大小为0表示不分片。此时分片数量为1，整集哈希为文件哈希。
//
type Head struct {
	Ver    byte   // 版本
	Span   int    // 分片大小（<=16MB）
	Amount uint32 // 分片数量
	Root   Hash32 // 整集哈希
}

//
// Size 头部占用字节数。
//
func (h *Head) Size() int {
	return lenHead
}

//
// Read 读取分片集头部定义。
// 外部需要保证读取流游标处于头部位置。
//
func (h *Head) Read(r io.Reader) error {
	var buf [8]byte
	if n, err := io.ReadFull(r, buf[:]); n != 8 {
		return err
	}
	h.Ver = buf[0]

	n3 := binary.BigEndian.Uint32(buf[0:4])
	h.Span = int(n3 & 0xffffff)

	h.Amount = binary.BigEndian.Uint32(buf[4:8])

	_, err := io.ReadFull(r, h.Root[:])
	return err
}

//
// Bytes 构造头部字节序列（40bytes）。
//
func (h *Head) Bytes() []byte {
	var buf [lenHead]byte

	binary.BigEndian.PutUint32(buf[:4], uint32(h.Span))
	buf[0] = h.Ver

	binary.BigEndian.PutUint32(buf[4:8], h.Amount)
	copy(buf[8:], h.Root[:])

	return buf[:]
}

//
// Sums 分片哈希集。
// 哈希存储键为源数据该分片的起始偏移值。
//
type Sums struct {
	Span int64            // 分片大小
	list map[int64]Hash32 // 分片哈希集[offset]。
}

//
// Size 分片集需要的字节数。
// 注：
// 仅自身，不包含应当存在的头部长度。
//
func (s Sums) Size() int {
	return len(s.list) * lenSum
}

//
// Empty 分片集是否为空。
//
func (s Sums) Empty() bool {
	return len(s.list) == 0
}

//
// Item 获取哈希条目。
// off参数为源数据分片的起始下标位置。
// 如果不存在目标位置的哈希存储，返回nil。
//
func (s Sums) Item(off int64) *Hash32 {
	if sum, ok := s.list[off]; ok {
		return &sum
	}
	return nil
}

//
// List 返回校验和清单。
//
func (s Sums) List() map[int64]Hash32 {
	return s.list
}

//
// Load 载入指定范围的分片哈希序列。
// rs为校验集输入源，其分片哈希按分片数据在原始文件内的偏移顺序排列。
// 注：输入源中包含全部分片的哈希存储。
//
// start/amount 指32字节的哈希单元计数。
// start指起始数量；amount表示载入的数量，-1表示载入全部。
//
// 返回io.EOF表示已读取到末尾。
//
func (s Sums) Load(rs io.ReadSeeker, start, amount int64) error {
	if s.Span <= 0 {
		return errZero
	}
	if s.list == nil {
		s.list = make(map[int64]Hash32)
	}
	if _, err := rs.Seek(start*lenSum+lenHead, io.SeekStart); err != nil {
		return err
	}
	if amount < 0 {
		amount = maxAmount
	}
	for ; start < amount; start++ {
		var sum Hash32
		if n, err := io.ReadFull(rs, sum[:]); n != lenSum {
			return err
		}
		s.list[start*s.Span] = sum
	}
	return nil
}

//
// Puts 输出分片哈希序列。
// 按偏移值的顺序存储各分片哈希。
// 返回输出的字节数和可能的输出/写入错误。
//
// 注：外部通常对w参数优化，比如采用bufio。
//
func (s Sums) Puts(w io.WriterAt) (int, error) {
	var err error
	var n, all int

	for off, sum := range s.list {
		cnt := off / s.Span
		n, err = w.WriteAt(sum[:], cnt*lenSum+lenHead)
		all += n
		if err != nil {
			break
		}
	}
	return all, err
}

//
// Offsets 返回偏移索引集。
// 返回的索引集是乱序的，如果未分片，返回nil。
//
func (s *Sums) Offsets() []int64 {
	if s.Span == 0 {
		return nil
	}
	buf := make([]int64, 0, len(s.list))

	for k := range s.list {
		buf = append(buf, k)
	}
	return buf
}

//
// Rests 剩余分片哈希集。
// 存储结构：
// 	[8+32][8+32]...
// 分片偏移值（8字节）存储在每一段32字节哈希之前。
//
// 仅用于下载过程中的状态存储，下载完毕即无用。
// 如果存在一个文件对应的剩余分片定义，则表示未下载完毕。
//
type Rests map[int64]Hash32

//
// Size 分片集存储需要的字节数。
//
func (r Rests) Size() int {
	return len(r) * (8 + 32)
}

//
// Empty 集合是否为空。
//
func (r Rests) Empty() bool {
	return len(r) == 0
}

//
// Item 获取哈希条目。
// off参数为源数据分片的起始下标位置。
// 如果不存在目标位置的哈希存储，返回nil。
//
func (r Rests) Item(off int64) *Hash32 {
	if sum, ok := r[off]; ok {
		return &sum
	}
	return nil
}

//
// Load 载入分片哈希数据。
// 分片数据偏移存储在每一段28字节序列的前8字节内。
// amount指28字节单元数，-1表示读取全部。
// 返回io.EOF表示已读取到末尾。
//
func (r Rests) Load(rs io.ReadSeeker, start, amount int) error {
	if amount < 0 {
		amount = maxAmount
	}
	if _, err := rs.Seek(int64(start*lenRest), io.SeekStart); err != nil {
		return err
	}
	for ; start < amount; start++ {
		var buf [lenRest]byte
		if n, err := io.ReadFull(rs, buf[:]); n != lenRest {
			return err
		}
		var sum Hash32
		copy(sum[:], buf[8:])

		r[int64(binary.BigEndian.Uint64(buf[:8]))] = sum
	}
	return nil
}

//
// Puts 输出分片哈希序列。
// 写入流游标应该在合适的位置（通常在起始位置）。
// 写入的单元包含了源数据的偏移，因此顺序无关紧要。
//
// 返回值为已写入的字节数和可能的错误。
// 外部通常会对w参数优化，比如采用bufio。
//
func (r Rests) Puts(w io.Writer) (int, error) {
	var cnt int
	var buf [lenRest]byte

	for off, sum := range r {
		binary.BigEndian.PutUint64(buf[:8], uint64(off))
		copy(buf[8:], sum[:])

		n, err := w.Write(buf[:])
		cnt += n
		if err != nil {
			return cnt, err
		}
	}
	return cnt, nil
}

//
// Piece 分片定义。
//
type Piece struct {
	Begin int64
	End   int64
}

//
// Size 分片大小（Begin～End）。
//
func (p Piece) Size() int {
	return int(p.End - p.Begin)
}

//
// Divide 分片配置服务。
// 对确定的数据大小进行分片定义，返回一个取值信道。
// 取值完成后，信道会被关闭。
// 会正确处理最后一个分片的结尾位置。
// 约束：
//  - 文件大小应该大于等于零；
//  - 分片大小大于文件大小时，即等于文件大小。
//
func Divide(fsize, span int64, stop *goes.Stop) <-chan Piece {
	if span > fsize {
		span = fsize
	}
	cnt, mod := fsize/span, fsize%span
	ch := make(chan Piece)

	go func() {
		for i := int64(0); i < cnt; i++ {
			off := i * span
			select {
			case <-stop.C:
				close(ch)
				return
			case ch <- Piece{off, off + span}:
			}
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
// 偏移&校验和值对。
// 用于 map[int64]Hash32 并发数据的传递。
//
type offSum struct {
	Off int64
	Sum Hash32
}

//
// Sumor 校验和生成器。
// 由 Do|FullDo 实施，仅可单次使用。
//
type Sumor struct {
	ra   io.ReaderAt
	list map[int64]Hash32
	ch   <-chan Piece
	vch  chan offSum
	end  *goes.Stop // 取值锁
}

//
// NewSumor 新建一个分片校验和生成器。
// 返回值仅可执行一次Do方法用于生成输入源的校验和。
// span 传递0或负值采用默认分片大小。
//
func NewSumor(ra io.ReaderAt, fsize, span int64) *Sumor {
	if span <= 0 {
		span = PieceSize
	}
	stop := goes.NewStop()
	sm := Sumor{
		ra:   ra,
		list: make(map[int64]Hash32),
		end:  stop,
		ch:   Divide(fsize, span, stop), // 一个分片服务
		vch:  make(chan offSum),
	}
	// 单独的赋值服务
	go func() {
		for v := range sm.vch {
			sm.list[v.Off] = v.Sum
		}
		sm.end.Exit()
	}()

	return &sm
}

//
// List 返回校验和清单。
// 需要在 Do 成功构建之后调用，否则阻塞。
//
func (s *Sumor) List() map[int64]Hash32 {
	<-s.end.C
	return s.list
}

//
// Task 获取一个分片定义。
// 实现 goes.Worker 接口。
//
func (s *Sumor) Task(over func() bool) (k interface{}, ok bool) {
	if !over() {
		k, ok = <-s.ch
	}
	return
}

//
// Work 计算一个分片数据的校验和。
// 实现 goes.Worker 接口。
//
func (s *Sumor) Work(k interface{}, over func() bool) error {
	if over() {
		return s.done(errExit)
	}
	p := k.(Piece)
	data, err := blockRead(s.ra, p.Begin, p.End)

	if err != nil {
		return err
	}
	if over() {
		return s.done(errExit)
	}
	s.vch <- offSum{
		p.Begin,
		sha256.Sum256(data),
	}
	return nil
}

//
// 工作结束。
// 返回传入的错误实参。
//
func (s *Sumor) done(err error) error {
	close(s.vch)
	return err
}

//
// Do 计算/设置分片校验和（有限并发）。
//
// 启动 limit 个并发计算，传递0采用内置默认值（SumThread）。
// 返回值为nil表示正确完成，否则为第一个错误信息。
//
func (s *Sumor) Do(limit int) error {
	defer s.done(nil)

	if limit <= 0 {
		limit = SumThread
	}
	return goes.Works(goes.LimitWorker(s, limit))
}

//
// SumChecker 校验和检查器。
// 用于完整文件依据校验清单整体核实。
// （内部并发检查）
//
type SumChecker struct {
	RA   io.ReaderAt
	Span int64
	List map[int64]Hash32
	ch   <-chan offSum
	end  *goes.Stop
}

//
// NewSumChecker 创建一个校验和检查器。
// 返回的检测器仅可执行一次Check或CheckAll，再次执行之前需Reset。
//
func NewSumChecker(ra io.ReaderAt, span int64, list map[int64]Hash32) *SumChecker {
	sc := SumChecker{
		RA:   ra,
		Span: span,
		List: list,
	}
	return sc.Reset()
}

//
// Reset 检查状态重置。
//
func (sc *SumChecker) Reset() *SumChecker {
	end := goes.NewStop()
	ch := make(chan offSum)

	go func() {
		for k, sum := range sc.List {
			select {
			case <-end.C:
				close(ch)
				return
			case ch <- offSum{k, sum}:
			}
		}
		close(ch)
	}()

	sc.ch = ch
	sc.end = end
	return sc
}

//
// Task 返回偏移下标。
// 实现 goes.Worker 接口。
//
func (sc *SumChecker) Task(over func() bool) (k interface{}, ok bool) {
	if !over() {
		k, ok = <-sc.ch
	}
	return
}

//
// Work 读取数据核实校验和。
// 不符合则返回一个错误。实现 goes.Worker 接口。
//
func (sc *SumChecker) Work(k interface{}, over func() bool) error {
	if over() {
		return errExit
	}
	os := k.(offSum)
	data, err := blockRead(sc.RA, os.Off, os.Off+sc.Span)

	if err != nil {
		return Error{os.Off, err}
	}
	if over() {
		return errExit
	}
	if sha256.Sum256(data) != os.Sum {
		return Error{os.Off, errChkSum}
	}
	return nil
}

//
// Check 计算校验。
// 简单计算，有任何一片不符即停止校验工作。
// 返回值非nil表示有错，值为首个错误信息。
//
func (sc *SumChecker) Check(limit int) error {
	defer sc.end.Exit()

	if limit <= 0 {
		limit = SumThread
	}
	return goes.Works(goes.LimitWorker(sc, limit))
}

//
// CheckAll 校验和完整计算对比。
// 返回校验和不符或读取错误的分片的源数据的起始下标集。
// 返回值为nil表示全部校验无错。
//
func (sc *SumChecker) CheckAll(limit int) []int64 {
	defer sc.end.Exit()

	if limit <= 0 {
		limit = SumThread
	}
	ech := goes.WorksLong(goes.LimitWorker(sc, limit), nil)

	var buf []int64

	for err := range ech {
		// 应当仅有Error类型
		buf = append(buf, err.(Error).Off)
	}
	return buf
}

//
// ChkSum 输入数据校验核实。
//
// 内部即为对SumChecker的使用，并发工作（并发量采用默认值）。
// span 为分块大小（bytes）。
//
func ChkSum(ra io.ReaderAt, span int64, list map[int64]Hash32) bool {
	return NewSumChecker(ra, span, list).Check(0) == nil
}

//
// Ordered 返回按偏移排序后的哈希序列。
// 已知分片大小的优化版。
//
// list需为完整的清单，如果下标偏移超出范围，返回nil。
// 主要用于哈希树根的计算。
//
func Ordered(span int64, list map[int64]Hash32) []Hash32 {
	buf := make([]Hash32, len(list))

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
