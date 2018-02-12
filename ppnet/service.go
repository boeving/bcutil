package ppnet

/////////////////////
/// DCP 接收服务实现。
/// 流程
/// 	客户端：应用请求 >> [发送]； [接收] >> 写入应用。
/// 	服务端：[接收] >> 询问应用，获取io.Reader，读取 >> [发送]。
///
/// 接收端
/// ------
/// 1. 接收对端的资源请求，完毕之后交付给响应器进行处理和发送。
/// 2. 接收对端发送来的响应数据（之前向对端请求过），写入应用接收接口（Receiver）。
///
/// - 根据应用接收接口的数据消化情况，决定ACK的发送：
///   > 如果消化处于忙状态，则停止发送ACK确认，缓存积累已收到的数据报。
///   > 如果消化处于饥饿状态，则ACK为全发模式，每收到一个数据报回馈一个确认。
///
/// - 根据发送距离因子，评估本端发送的ACK丢失情况，可能重新确认丢失的ACK。
/// - 根据传输路径超时（参考两个连续包的间隔时间），请求缺失的某个包。
/// - 如果已经收到END包，则之后每收到一个包就请求一次中间缺失的包。
///
///
/// 并行接收
/// - 每个数据体内的分组由序列号表达顺序，并按此重组数据。
/// - 发送方数据体的发送是一种并发，因此接收到的数据体类似于一种并行。
///   这种情况下，小数据体不会受到大数据体的阻塞，会先完成。
///
/// 发送控制
/// - 接收端也有发送的控制权，实际上接收端才是数据发送的主导者。
/// - 每个数据体的应用消化各不相同。通过对数据确认的控制，抑制发送方的速率。
/// - 如果已经收到END包但缺失中间的包，主动请求重发可以尽快完成数据体（交付到应用）。
/// - 从「发送距离」评估确认是否送达（ACK丢失），必要时重新确认。
///
///////////////////////////////////////////////////////////////////////////////

import (
	"errors"
	"io"
	"log"
	"net"
	"time"

	"github.com/qchen-zh/pputil/goes"
	"golang.org/x/tools/container/intsets"
)

// 基础常量设置。
const (
	AliveProbes  = 6                      // 保活探测次数上限
	AliveTime    = 120 * time.Second      // 保活时间界限，考虑NAT会话存活时间
	AliveIntvl   = 10 * time.Second       // 保活报文间隔时间
	CalmExpire   = 90 * time.Second       // 服务静置期限（SND/RCV子服务无活动）
	ReadyTimeout = 10 * time.Second       // 资源请求发送完毕，等待接收超时
	ReadyRspTime = 500 * time.Millisecond // 请求响应起始间隔时间
	ReadyRspMax  = 5                      // 请求响应最多次数
	RtpTimeout   = 60 * time.Second       // 相同重发请求积累超时
	RtpEndTime   = 1 * time.Second        // BYE等待超时重发间隔
	ByeTimeout   = 6 * time.Second        // BYE等待超时重发间隔（应当小于ReadyTimeout）
)

// 基本参数常量
const (
	xLimit32     = 1<<32 - 1 // 序列号上限（不含）
	xLimit16     = 1<<16 - 1 // 数据体ID上限
	recvTimeoutx = 2.5       // 接收超时的包间隔倍数
	getsChSize   = 2         // 网络数据向下传递信道缓存
)

var (
	errResponser = errors.New("not set Responser handler")
)

//
// service 基础服务。
// 接收网络数据，转发到数据体的接收服务器。
// 如果为一个资源请求，创建一个发送服务器（servSend）交付。
//
// 一个对端4元组连系对应一个本类实例。
//
type service struct {
	Resp  Responser      // 响应器
	dcps  *dcps          // 子服务管理器
	clean func(net.Addr) // 接收到断开后的清理（Listener:pool）
}

func newService(r Responser, dx *dsps, clean func(net.Addr)) *service {
	return &service{
		Sndx:    newXSender(w, ch),
		Pch:     ch,
		SndPool: make(map[uint16]*servSend),
		RcvPool: make(map[uint16]*recvServ),
		clean:   clean,
	}
}

//
// 开启接收服务。
//
func (s *service) Start() *service {
	//
}

//
// 递送数据报。
// 由监听器读取网络接口解析后分发。
// 判断数据报是资源请求还是对本端资源请求的响应。
//
// 向接收器传递数据：
// - 资源请求包含REQ标记。交由资源请求接收器
// - 无REQ标记的为响应数据，交由响应接收器。
//
// 向发送器传递确认信息：
// - 向ackRecv服务传递确认号和确认距离（rcvInfo）。
//
func (s *service) Post(pack *packet) {
	//
}

//
// 检查数据报头状态，提供相应的操作。
//
func (s *service) Checks(h *header) {
	//
}

//
// 总接受服务结束。
//
func (s *service) Exit() error {
	//
}

//
// 获取响应读取器。
//
func (s *service) resReader(res []byte) (io.Reader, error) {
	if s.Resp == nil {
		return nil, errResponser
	}
	return s.Resp.GetReader(res)
}

//
// 新建一个接收服务。
// 它由一个新的请求激发，发送请求。
// 初始化一个随机序列号，创建一个接收服务。
//
func (s *service) newReceive(res []byte, rec Receiver) {
	//
}

//
// 确认或重发请求申请。
// 用于接收器提供确认申请或重发请求给发送总管。
// 发送总管将信息设置到数据报头后发送（发送器提供）。
//
type ackReq struct {
	ID   uint16 // 数据ID#RCV
	Ack  uint32 // 确认号
	Dist uint8  // 确认距离（0值无漏包）
	Rtp  bool   // 重发请求
	Req  bool   // 资源请求的确认
}

type ackDst struct {
	Ack  uint32 // 确认号
	Dist uint8  // 确认距离
}

//
// 发送数据关联信息。
// 从对端发送过来的请求或响应中提取的信息。
// - 为资源请求时，用于缓存请求数据标识（之后向响应器提交）。
// 用于请求缓存或应用接收器消耗数据并提供进度控制。
//
type sndInfo struct {
	Seq      uint32 // 序列号
	Data     []byte // 响应数据
	Beg, End bool   // 首位数据报标记
	Req      bool   // 为资源请求
}

//
// 接收服务清理器器。
//
type rcvCleaner func(uint16)

//
// 接收服务器。
// A: 接收对端的资源请求，完毕之后交付到响应器处理。
// B: 接收对端的响应数据，写入请求时附带的接收接口。
//
// 向发送总管提供确认或重发申请。一个数据体对应一个本类实例。
//
// - 等待对端响应，超时后重新请求初始分组。
// - 根据应用接收数据的情况确定进度号，约束对端发送速率。
// - 当接收到END包或路径超时时，申请中间缺失包的重发。
// - 当接收到BYE包，结束接收服务。
//
// 接收项：
// - Seq  	响应数据序列号
// - Data 	响应数据
// - END 	END包标记（发送完成）
// - BYE 	BYE信息（结束）
//
type recvServ struct {
	ID    uint16          // 数据ID#RCV（<=ID#SND）
	SndIn chan<- *sndInfo // 接受到的发送数据信息
	RtpIn chan<- *rtpInfo // 请求重发所需评估信息

	ackReq chan<- *ackReq // 确认申请（-> xSender）
	ackEv  *ackEval       // 确认评估器
	rtpEv  *rtpEval       // 重发评估器
	isReq  bool           // 为资源请求接收
	alive  time.Time      // 活着时间戳
}

//
// 新建一个接收子服务。
// - 应当仅在请求发送完毕后创建一个响应接收。
// - 或收到一个不同的请求时创建一个请求接收。
//
// ack 为所期待的对端响应的首个分组的序列号。
// 注：也即资源请求的最后一个分组的确认号（预设约定）。
// done 为接收完毕或中途退出时的调用。
//
func newRecvServ(id uint16, ack uint32, ar chan<- *ackReq, rc Receiver, isReq bool) *recvServ {
	sndCh := make(chan *sndInfo, getsChSize)
	rtpCh := make(chan *rtpInfo, getsChSize)

	return &recvServ{
		ID:     id,
		SndIn:  sndCh,
		RtpIn:  rtpCh,
		ackReq: ar,
		ackEv:  newAckEval(sndCh, newAppReceive(rc)),
		rtpEv:  newRtpEval(ack, rtpCh),
		isReq:  isReq,
		alive:  time.Now(),
	}
}

//
// 启动接收服务。
// 启动重发和确认评估模块，接收评估汇总。
// 构造确认申请向 xSender 递送。
//
func (r *recvServ) Serve(exit *goes.Stop, done rcvCleaner) {
	defer done(r.ID)

	// 评估模块控制
	stop := goes.NewStop()
	defer stop.Exit()

	ackCh := make(chan ackDst, 1)
	rtpCh := make(chan uint32, 1)

	go r.ackEv.Serve(ackCh, stop)
	go r.rtpEv.Serve(rtpCh, stop)

	for {
		req := r.newAckReq()

		select {
		// 上级收到BYE消息后主动结束
		case <-exit.C:
			return

		case ack := <-rtpCh:
			req.Ack = ack
			req.Rtp = true

		case ad, ok := <-ackCh:
			if !ok {
				return // 等待BYE超时
			}
			req.Ack = ad.Ack
			req.Dist = ad.Dist
		}
		r.ackReq <- req
		r.alive = time.Now()
	}
}

//
// 新建一个空确认请求。
//
func (r *recvServ) newAckReq() *ackReq {
	return &ackReq{
		ID:  r.ID,
		Req: r.isReq,
	}
}

//
// 服务存活判断。
// 如果数据体接收停止时间过长，会被视为已死亡。
// 它通常由BYE信息丢失或发送异常终止导致（上层无法即时清理）。
//
// 这会占用数据体ID资源，上层在分配数据体ID时据此检查。
//
func (r *recvServ) Alive() bool {
	return time.Since(r.alive) < CalmExpire
}

//
// 确认评估器。
// - 由应用接收器对数据的消耗，制约进度确认。
// - 超时重发END包确认（如果上层未主动结束的话）。
//
type ackEval struct {
	Snd  <-chan *sndInfo // 响应数据递送
	App  *appReceive     // 应用接收器
	pool *recvPool       // 数据接收管理
	beg  bool            // 已接收BEG包
	endx uint32          // END包确认号
}

func newAckEval(snd <-chan *sndInfo, ar *appReceive) *ackEval {
	return &ackEval{
		Snd:  snd,
		App:  ar,
		pool: newRecvPool(),
	}
}

//
// 启动一个确认评估服务。
// - 收到的数据报构成有效进度时，等待应用消耗完才发送确认。
// - 收到的数据报无法填充空缺时，会立即发送先前的进度确认。
//
// ach 为确认号递送信道。
// END重复确认超时后会关闭ach信道。
//
func (a *ackEval) Serve(ach chan<- ackDst, exit *goes.Stop) {
	// END重复确认
	var endAck <-chan time.Time
	wait := RtpEndTime

	for {
		select {
		// 通常由上层收到BYE后触发
		case <-exit.C:
			return

		case <-endAck:
			if wait > ByeTimeout {
				log.Println("wait BYE message timeout.")
				close(ach)
				return
			}
			ach <- ackDst{a.endx, 0}
			wait += wait
			endAck = time.After(wait)

		// BYE信息并不传递至此
		case si := <-a.Snd:
			a.update(si)
			if !a.beg {
				continue
			}
			ack := a.pool.Acked()
			bs, done := a.pool.ToAck(ack)

			// 等待消耗或立即
			if bs != nil {
				a.App.Puts(bs, done)
			}
			ach <- ackDst{ack, a.pool.Dist()}

			// 结尾收场
			if done {
				endAck = time.After(wait)
			}
		}
	}
}

//
// 更新接收池信息。
// 注：计算为确认号后更新更简单。
//
func (a *ackEval) update(si *sndInfo) {
	ack := roundPlus(si.Seq, len(si.Data))

	if si.End {
		a.pool.End()
		a.endx = ack
	}
	if si.Beg {
		a.beg = true
		a.pool.First(ack, si.Data)
		return
	}
	a.pool.Add(ack, si.Data)
}

//
// 接收队列。
// 处理接收到的数据不连续的问题。
//
type recvPool struct {
	acks *roundQueue       // 确认号有序集
	pool map[uint32][]byte // 数据存储（key:ack）
	end  bool              // 已接收到END包
}

func newRecvPool() *recvPool {
	return &recvPool{pool: make(map[uint32][]byte)}
}

//
// 设置END包已收到。
//
func (p *recvPool) End() {
	p.end = true
}

//
// 接收到首个分组（BEG）。
// 注：acks为延迟创建。本方法应当只调用一次。
//
func (p *recvPool) First(ack uint32, b []byte) {
	p.acks = newRoundOrder(ack)
	// 可能之前添加
	for s := range p.pool {
		p.acks.Push(s)
	}
	p.pool[ack] = b
}

//
// 压入新的确认号及其所确认的数据。
//
func (p *recvPool) Add(ack uint32, b []byte) {
	if p.acks != nil {
		p.acks.Push(ack)
	}
	p.pool[ack] = b
}

//
// 移动到进度线。
// 返回确认号之前的连续数据片和是否已全部接收完。
//
// 需要处理数据长度超出进度的部分。
// 这是因为路径MTU变化后，发送方重发产生了重叠部分。
// ack 为对应确认号的数据片序列号。
//
// 注：
// 会清除确认号之前的历史数据。
//
func (p *recvPool) ToAck(ack uint32) ([][]byte, bool) {
	var bs [][]byte
	buf := p.acks.Queue()
	beg := p.acks.Beg()

	for i := 0; i < p.acks.Clean(ack); i++ {
		ack = buf[i]
		b := p.pool[ack]

		// 有效部分
		sz := roundSpacing(beg, ack)
		bs = append(bs, b[len(b)-sz:])

		beg = ack
		delete(p.pool, ack)
	}
	return bs, p.end && p.acks.Size() == 0
}

//
// 确认检查。
// 检查并返回当前进度的确认号。
// 重叠部分视为连续，调用者需自行处理重叠部分。
//
// 注记：
// - 上一个数据的确认号与下一个数据的序列号相等即为连续。
// - 上一个数据的确认号后于下一个数据的序列号，则为部分重叠。
//
func (p *recvPool) Acked() uint32 {
	beg := p.acks.Beg()
	ack := beg
	buf := p.acks.Queue()

	for i := 0; i < len(buf); i++ {
		a2 := buf[i]
		seq := roundBegin(a2, len(p.pool[a2]))
		if roundOrder(beg, ack, seq) < 0 {
			break
		}
		ack = a2
	}
	return ack
}

//
// 确认距离。
// 收到起始分组之前，距离即收到的计数（1+）。
// 通常，外部应当先调用ToAck，然后获取确认距离。
//
func (p *recvPool) Dist() uint8 {
	d := 0
	if p.acks == nil {
		d = len(p.pool)
	} else {
		d = p.acks.Size()
	}
	if d > 0xff {
		log.Printf("ACK distance %d is overflow.", d)
	}
	return uint8(d)
}

//
// 客户端接收处理。
// 分离为一个单独的处理类，用于写入阻塞回馈。
//
type appReceive struct {
	Recv  Receiver // 接收器实例
	total int64    // 已写入数据量累计
}

func newAppReceive(rc Receiver) *appReceive {
	return &appReceive{Recv: rc}
}

//
// 连续的分组输出。
// 写入出错或最终结束（END），会执行应用接收器的关闭接口。
//
func (a *appReceive) Puts(ds [][]byte, end bool) error {
	var err error

	for _, data := range ds {
		n, err = a.Recv.Write(data)
		a.total += n

		if err != nil {
			log.Printf("%v on receive %d bytes.", err, a.total)
			break
		}
	}
	if err != nil || end {
		err = a.Recv.Close()
	}
	return err
}

//
// RTP关联信息。
// 用于传入评估服务计算重发申请。
//
type rtpInfo struct {
	Seq, Ack uint32 // 确认号
	End      bool   // 传输完成（END）
}

//
// 重发申请评估。
// 评估条件状况，决定是否申请重发。
// 条件：
// - 超时。前两个包的间隔的2-3倍。
// - END包到达后，间断请求确认号的目标包（结合新到包）。
//
// 如果收到END包且没有漏掉的包，则关闭通知信道。
//
type rtpEval struct {
	Info      <-chan *rtpInfo // 基本响应信息通道
	seqx      intsets.Sparse  // 已收到记录
	ackx      intsets.Sparse  // 确认号剩余
	end       bool            // 已收到END包
	last, cur time.Time       // 上一个与当前包的接收时间
}

//
// 创建一个重发申请评估器。
// ack0 为初始确认号（所需BEG包序列号）。
//
func newRtpEval(ack0 uint32, ri <-chan *rtpInfo) *rtpEval {
	tm := time.Now()

	re := rtpEval{
		Info: ri,
		last: tm.Add(-ReadyTimeout),
		cur:  tm,
	}
	re.ackx.Insert(int(ack0))

	return &re
}

//
// 执行评估服务。
// rch 为请求重发数据报的序列号通知信道。
//
func (r *rtpEval) Serve(rch chan<- uint32, exit *goes.Stop) {
	// 路径超时
	out := time.NewTimer(r.timeOut())
	defer out.Stop()

	// 重发累计次数
	var rcnt int

	for {
		select {
		case <-exit.C:
			return

		case <-out.C:
			rcnt++
			if time.Since(r.last) > RtpTimeout {
				log.Printf("retransmit %d tries, timeout at last.", rcnt)
				return
			}
			r.lossRtp(rch)
			// 积累延迟（2^n-1）
			r.cur = time.Now()

		case ri := <-r.Info:
			r.update(ri)

			if r.done() {
				return // no close(rch)
			}
			if r.end {
				r.endRtp(rch)
			}
		}
		out.Reset(r.timeOut())
	}
}

//
// 统计状态更新。
// 确认号的统计需排除END包，已便于done检测。
//
func (r *rtpEval) update(ri *rtpInfo) {
	r.last = r.cur
	r.cur = time.Now()

	if !r.seqx.Insert(int(ri.Seq)) {
		return
	}
	if ri.End {
		r.end = true
		// not insert.
	} else {
		r.ackx.Insert(int(ri.Ack))
	}
	r.ackx.Remove(int(ri.Seq))
}

//
// 超时时间。
// 用最后两个包收到的时间间隔计算超时（2-3倍）。
//
func (r *rtpEval) timeOut() time.Duration {
	return time.Duration(
		float64(r.cur.Sub(r.last)) * recvTimeoutx,
	)
}

//
// 收到END包后的重发申请。
// 注：与超时重发不重复。
//
func (r *rtpEval) endRtp(rch chan<- uint32) {
	// 留给超时机制
	if r.ackx.Len() == 1 {
		return
	}
	rch <- uint32(r.ackx.Max())
}

//
// 漏包重发。
// 请求的中间缺失的包的顺序没有定义。
//
func (r *rtpEval) lossRtp(rch chan<- uint32) {
	if r.ackx.Len() == 0 {
		return
	}
	rch <- uint32(r.ackx.Min())
}

//
// 全部接收完成。
//
func (r *rtpEval) done() bool {
	return r.end && r.ackx.Len() == 0
}
