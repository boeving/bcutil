package ppnet

///////////////////////////////////
/// DCP 发送服务普通版（多个数据报）。
/// 一个数据体至少需要两个数据报发送的，存在数据报间的连贯性需求。
///////////////////////////////////////////////////////////////////////////////

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/qchen-zh/pputil/goes"
)

// 基础常量设置。
const (
	SendEndtime = 10 * time.Second       // 发送END包后超时结束时限
	SendAckTime = 600 * time.Millisecond // 首个确认等待时间
)

var (
	errLossOffset = errors.New("bad offset on loss reacquire")
)

var (
	subEaseRate = newEaseRate(sendRateTotal) // 子发送服务速率计算器
)

//
// 发送确认信息。
//
type ackLoss struct {
	Ack uint32 // 当前进度线确认号
	Off int    // 发送数据相对于进度线的偏移
}

//
// 序列号/确认号对。
//
type seqAck struct {
	Seq, Ack uint32
}

//
// 确认信息。
// 对端对本端响应的确认信息。
//
type rcvInfo struct {
	Ack  uint32 // 确认号
	Dist int    // 确认距离
	Rtp  bool   // 重传请求
}

//
// 数据报发送器。
// 一个响应服务对应一个本类实例。
// - 从应用响应器读取数据，构造数据报递送到总发送器。
// - 根据对端确认距离评估丢包和重发。
//
// 退出：
// - 接收到END确认后（ackRecv），发送一个BYE通知即退出。
// - 接收端收到BYE之前会重复多次END确认，如果全部丢失，则由超时机制退出。
//
// 注记：
// 丢失的包不可能跨越一个序列号回绕段，太大的发送距离会让发送停止。
//
type servSend struct {
	ID    uint16          // 数据ID#SND
	RcvIn chan<- *rcvInfo // 接收确认信息（<- net）

	iPost chan<- *packet // 数据报递送（-> xServer）
	iBye  chan<- *ackBye // 结束通知（BYE -> xServer）
	cLoss chan *ackLoss  // 丢包重发通知（offset）
	resp  responser      // 响应器
	eval  *evalRate      // 速率评估器
	recv  *ackRecv       // 接收确认
	dcnt  *sendDister    // 发送距离计数器

	read  chan *packet  // 读取回环传递（buf>0）
	isBeg bool          // 是否为首个包
	isReq bool          // 为资源请求发送
	seq   uint32        // 当前序列号
	end   uint32        // END包的确认号
	endCh chan<- seqAck // END包信息传递（-> ackRecv）
}

//
// 新建一个子发送服务器。
// seq 实参为一个初始首个包的序列号。
//
func newServSend(id uint16, seq uint32, rsp responser, pch chan<- *packet, bch chan<- ackBye, dch chan<- int) *servSend {
	dcnt := &sendDister{}
	lsev := newLossEval(subEaseRate, sendRateZoom)
	endc := make(chan seqAck, 1)
	rcvi := make(chan *rcvInfo, getsChSize)

	return &servSend{
		ID:    id,
		RcvIn: rcvi,
		iPost: pch,
		iBye:  bch,
		cLoss: make(chan *ackLoss),
		resp:  rsp,
		read:  make(chan *packet, 1), // 缓存>0
		eval:  newEvalRate(lsev, sendRateZoom, subEaseRate, dch),
		recv:  newAckRecv(rcvi, endc, lsev, rsp, dcnt),
		isBeg: true,
		seq:   seq,
		dcnt:  dcnt,
		end:   xLimit32,
		endCh: endc,
	}
}

//
// 设置为资源请求类型。
//
func (s *servSend) SetReq() {
	s.isReq = true
}

//
// 启动发送服务（阻塞）。
// 丢失的包简单重传，不受速率限制立即发送。
// 传入的exit用于外部终止服务。
// 注记：
// 丢包与正常的发送在一个Go程里处理，无并发问题。
//
func (s *servSend) Serve(re *rateEval, exit *goes.Stop, done func(uint16, uint32)) {
	// 确认接收分支服务。
	stop := goes.NewStop()
	go s.recv.Serve(s.cLoss, re, stop)
	defer stop.Exit()

	// 等待END确认超时
	out := time.NewTimer(SendEndtime)
	defer out.Stop()

	for {
		var p *packet
		size := PayloadSize()

		select {
		case <-exit.C:
			return // 总结束
		case <-out.C:
			// 通常不至于此。
			log.Printf("wait id:[%d] end ACK timeout.", s.ID)
			s.toBye(done)
			return

		case as, ok := <-s.cLoss:
			if !ok {
				s.toBye(done)
				return // END确认，正常结束
			}
			if as.Off < 0 {
				log.Printf("invalid offset by lost sequence.\n")
				continue
			}
			s.iPost <- s.lossBuild(as.Ack, size, as.Off)

		// 结束后阻塞，等待确认。
		// 不影响丢包重发。
		case p := <-s.Send(size):
			s.iPost <- p
			s.update(p)

			// 速率控制
			time.Sleep(s.eval.Rate(s.dcnt.Dist(), re.Base()))
		}
		out.Reset(SendEndtime)
	}
}

//
// 发送结束通知（BYE）
// 因为无数据负载，BYE作为一个确认信息发出。
//
func (s *servSend) toBye(done func(uint16, uint32)) {
	go func() {
		s.iBye <- ackBye{
			ID:  s.ID, // => #RCV
			Ack: rand.Uint32(),
		}
	}()
	done(s.ID, s.end)
}

//
// 状态更新。
// 设置下一个序列号，同时会更新距离计数器。
// 注记：
// 在数据报进入发送队列之后更新，减小END确认超时延误。
//
func (s *servSend) update(p *packet) {
	ack := roundPlus(s.seq, p.Size())

	if p.BEG() {
		s.dcnt.Init(ack)
	} else {
		s.dcnt.Add(ack)
	}
	if p.END() {
		// 向ackRecv传递END包信息
		// no close
		s.endCh <- seqAck{s.seq, s.end}
	}
	s.seq = ack // next..
}

//
// 返回一个正常发送数据报的读取信道。
// 传递并返回通道，主要用于读取结束后的阻塞控制。
//
func (s *servSend) Send(size int) <-chan *packet {
	if s.read == nil {
		return nil
	}
	// buffer size > 0
	s.read <- s.Build(s.seq, size)
	return s.read
}

//
// 正常构建数据报。
// 从响应器获取数据片构造数据报。
// 返回nil表示读取出错（非io.EOF）。
//
func (s *servSend) Build(seq uint32, size int) *packet {
	b, err := s.resp.Get(size)
	if b == nil {
		// 正常发送完毕
		s.read = nil
		return nil
	}
	if err != nil && err != io.EOF {
		log.Printf("read [%d] error: %s.\n", s.ID, err)
		return nil
	}
	if err == io.EOF {
		s.end = roundPlus(seq, len(b))
	}
	return s.build(b, seq, err == io.EOF)
}

//
// 丢包后的数据报构建。
// off 为-1时表示END包，会即时计算seq值（可能PMTU变化）。
//
func (s *servSend) lossBuild(seq uint32, size, off int) *packet {
	b, err := s.resp.LossGet(size, off)
	if err != nil {
		log.Println(err)
		return nil
	}
	if off == -1 {
		seq = roundBegin(s.end, len(b))
	}
	return s.build(
		b,
		seq,
		roundPlus(seq, len(b)) == s.end,
	)
}

//
// 构建数据报。
//
func (s *servSend) build(b []byte, seq uint32, end bool) *packet {
	h := newHeader(s.ID, seq)
	if end {
		h.Set(END)
	}
	if s.isBeg {
		h.Set(BEG)
		s.isBeg = false
	}
	if s.isReq {
		h.Set(REQ)
	}
	return &packet{header: h, Data: b}
}

//
// 确认接收器。
// 接收对端的确认信息，评估丢包和重发通知。
// - 丢包或对端主动请求重发时，通知目标数据包的序列号。
// - 如果收到END数据报的确认，关闭通知信道。
//
type ackRecv struct {
	Rcvs  <-chan *rcvInfo // 接收信息传递信道
	Endx  <-chan seqAck   // END包信息（发送）
	Loss  *lossEval       // 丢包评估器
	Resp  *response       // 响应器引用
	Dcnt  *sendDister     // 距离计数器引用
	acked uint32          // 确认号（进度）存储
}

//
// acked 初始化为一个无效值。
//
func newAckRecv(rch <-chan *rcvInfo, ech <-chan seqAck, le *lossEval, rsp *response, sd *sendDister) *ackRecv {
	return &ackRecv{
		Rcvs:  rch,
		Endx:  ech,
		Loss:  le,
		Resp:  rsp,
		Dcnt:  sd,
		acked: xLimit32,
	}
}

//
// 启动接收&评估服务。
// 传递丢包序列号和与进度线的偏移值。
//
// ach 通知当前进度确认号和丢包序列号偏移值（相对于进度线）。
// 当收到END信息时关闭通知信道，发送器据此退出服务。
//
func (a *ackRecv) Serve(ach chan<- *ackLoss, re *rateEval, exit *goes.Stop) {
	// END包信息
	var seq, end uint32
	var out <-chan time.Time

	// END包确认等待
	// 注：实际上为针对包全部丢失情况。
	var wait time.Duration

	for {
		select {
		case <-exit.C:
			return

		case sa := <-a.Endx:
			seq = sa.Seq
			end = sa.Ack
			a.Endx = nil
			// END包被发送后，开启超时
			out = time.After(SendAckTime)

		case <-out:
			// 重发指数退避
			wait += wait
			if wait > SendTimeout {
				log.Println("wait Acknowledgment was timeout.")
				return
			}
			ach <- &ackLoss{seq, -1}
			out = time.After(wait)

		case ai := <-a.Rcvs:
			if ai.Rtp {
				// Ack为目标序列号
				ach <- a.sendAck(ai.Ack)
				continue
			}
			if ai.Ack == end {
				close(ach)
				return
			}
			// 确认进度。
			a.update(ai.Ack)

			if a.lost(ai.Dist, re) {
				// Ack为进度线确认号
				ach <- a.sendAck(ai.Ack)
			}
			out = nil // 不再需要
		}
	}
}

//
// 更新当前进度。
// - 更新当前确认号（进度）。
// - 对响应器已确认历史的移动清理。
// - 清理距离计数器。
//
func (a *ackRecv) update(ack uint32) {
	if a.acked == ack {
		return
	}
	a.acked = ack
	a.Dcnt.Clean(ack)
	a.Resp.Acked(roundSpacing(a.acked, ack))
}

//
// 评估丢包。
// 子服务丢包判断会间接影响总速率评估器。
//
// dist 为确认距离，re 为总速率评估器。
//
func (a *ackRecv) lost(dist int, re *rateEval) bool {
	lost := a.Loss.Eval(float64(dist), re.Base())
	if lost {
		// 丢包影响
		re.Loss <- struct{}{}
	}
	return lost
}

//
// 构造确认信息包。
// 如果首个包即丢失，ack固定为xLimit32。
//
func (a *ackRecv) sendAck(ack uint32) *ackLoss {
	var off int

	if ack != a.acked {
		off = roundSpacing(a.acked, ack)
	}
	return &ackLoss{a.acked, off}
}

//
// 响应器接口。
//
type responser interface {
	// 正常获取数据。
	Get(size int) ([]byte, error)

	// 获取丢包数据
	// 已经发送过，丢包后从历史栈中检索。
	LossGet(size, off int) ([]byte, error)
}

//
// 请求响应器。
// 提供对端请求需要的响应数据。
// 内部管理一个已发送缓存，用于丢包重发时提取数据。
// 当一个确认到达时，外部应当调用Acked以清理不再需要的数据。
// 一个数据体对应一个本类实例。
//
type response struct {
	br   *bufio.Reader // 响应读取器
	sent *bytes.Buffer // 确认前发送暂存
	done bool          // 读取结束
}

func newResponse(r io.Reader) *response {
	return &response{
		br:   bufio.NewReader(r),
		sent: bytes.NewBuffer(nil),
	}
}

//
// 确认回收。
// 外部应当保证回收的数量正确，否则会导致panic。
// amount 为回收的数量，即与上一次确认点的字节偏移量。
//
func (r *response) Acked(amount int) {
	if amount == 0 {
		return
	}
	if amount > r.sent.Len() {
		panic("too large of Ack offset.")
	}
	r.sent.Next(amount)
}

//
// 丢包重发时的重新获取。
// 因为路径MTU可能改变，因此缓存中可能不是size的整数倍。
// 最后一片数据大小可能小于size。
//
// size 为所需数据片大小（最大值，字节数）。
// off 为丢包所需数据起点相对于确认点的字节偏移，负值表示末尾。
//
// 注记：
// 返回的数据量在size范围内，但尽量多。
// 实际上已包含了重新组包的逻辑（repacketization）。
//
func (r *response) LossGet(size, off int) ([]byte, error) {
	b := r.sent.Bytes()

	if off >= len(b) {
		return nil, errLossOffset
	}
	if off == -1 {
		return r.last(b, size), nil
	}
	if size+off >= len(b) {
		return b[off:], nil
	}
	return b[off : off+size], nil
}

//
// 获取末尾size大小的分片。
//
func (r *response) last(buf []byte, size int) []byte {
	if size >= len(buf) {
		return buf
	}
	return buf[len(buf)-size:]
}

//
// 正常的获取数据。
// 返回最后一片数据时，第二个参数必然为io.EOF。
//
func (r *response) Get(size int) ([]byte, error) {
	if r.done {
		return nil, nil
	}
	b, err := r.read(size)
	if b == nil {
		return nil, err
	}
	// 已发送暂存
	if _, err = r.sent.Write(b); err != nil {
		return nil, err
	}
	return b, err
}

//
// 从原始的读取器中获取数据。
// 如果读取到数据尾部，io.EOF保证与最后一片数据同时返回。
//
// 如果一开始就没有可读的数据，返回的数据片为nil。
//
func (r *response) read(size int) ([]byte, error) {
	buf := make([]byte, size)
	n, err := io.ReadFull(r.br, buf)

	if n == 0 {
		r.done = true
		return nil, err
	}
	if err == nil {
		// 末尾探测
		_, err = r.br.Peek(1)
	}
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	if err == io.EOF {
		r.done = true
	}
	return buf[:n], err
}

//
// 发送距离计数。
// 主要用于发送速率的评估，发送距离不传递给对端。
// 注记：
// 发送时的记录和收到确认时的清理在不同的Go程中。
//
type sendDister struct {
	buf *roundQueue // 确认队列
	mu  sync.Mutex  // 清理保护
}

//
// 初始确认号（进度）。
//
func (d *sendDister) Init(ack uint32) {
	d.buf = newRoundOrder(ack)
}

//
// 添加一个数据报计数。
// ack 为确认号（下一个数据报序列号）。
//
func (d *sendDister) Add(ack uint32) {
	d.mu.Lock()
	d.buf.Push(ack)
	d.mu.Unlock()
}

//
// 进度之前的历史清理。
//
func (d *sendDister) Clean(ack uint32) {
	d.mu.Lock()
	d.buf.Clean(ack)
	d.mu.Unlock()
}

//
// 返回发送距离。
//
func (d *sendDister) Dist() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.buf.Size()
}
