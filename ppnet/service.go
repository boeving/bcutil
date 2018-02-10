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
	ByeTimeout   = 10 * time.Second       // BYE等待超时重发间隔
)

// 基本参数常量
const (
	xLimit32     = 1<<32 - 1 // 序列号上限（不含）
	xLimit16     = 1<<16 - 1 // 数据体ID上限
	recvTimeoutx = 2.5       // 接收超时的包间隔倍数
	reAckLimit   = 3         // 更新确认的无效进度上限
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
	Resp  Responser       // 响应器
	Recv  chan<- *rcvInfo // 信息递送通道
	dcps  *dcps           // 子服务管理器
	clean func(net.Addr)  // 接收到断开后的清理（Listener:pool）
}

func newService(w *connWriter, clean func(net.Addr)) *service {
	ch := make(chan *packet, sendPackets)

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
// 由监听器读取网络接口解析后分发（并发安全）。
//
// 判断数据报是对端的资源请求还是对端对本端资源请求的响应。
// - 资源请求包含REQ标记。交由响应&发送器接口。
// - 无REQ标记的为响应数据报，交由接收器接口。
// - 如果数据报有BYE标记，则取接收数据ID传递到接收接口。
//
// - 向响应接口（ackRecv）传递确认号和确认距离（rcvInfo）。
// - 向响应接口（servSend）传递重置发送消息。
//
// - 向接收器接口传递数据&序列号和发送距离。
// - 向接收器接口传递重置接收指令，重新接收部分数据。
//
// - 如果重置发送针对整个数据体，新建一个发送器实例执行。
// - 如果重置接收针对整个数据体，新建一个接收器服务执行。
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
// 末尾包确认服务。
// 各数据体末尾END包确认的持续服务，保证发送方的结束。
//
// 如果收到BYE包则停止，并可移除其END条目。
// 如果未收到BYE包，则按评估的速率重发END确认，最多endAcks次。
//
type endAcks struct {
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
	Qer  bool   // 资源请求的确认
}

//
// 子接收/确认服务所需参数备存。
// 用于创建 recvServ 实例的成员。
//
type forAcks struct {
	AckReq chan *ackReq // 确认申请（-> xSender）
	Rtp    chan int     // 请求重发通知
	Rack   chan int     // 再次确认通知
	Ack    chan int     // 应用确认通知（数据消耗）
}

func newForAcks() *forAcks {
	return &forAcks{
		AckReq: make(chan *ackReq, PostChSize),
		Rtp:    make(chan int, 1),
		Rack:   make(chan int, 1),
		Ack:    make(chan int, 1),
	}
}

type ackDst struct {
	Ack  uint32 // 确认号
	Dist uint   // 确认距离
}

//
// 接收服务器。
// A: 接收对端的资源请求，完毕之后交付到响应器处理。
// B: 接收对端的响应数据，写入请求时附带的接收接口。
//
// 向发送总管提供确认或重发申请。一个数据体对应一个本类实例。
//
// - 等待对端响应，超时后重新请求初始分组。
// - 根据应用接收数据的情况确定进度号，约束对端发送速率。
// - 评估对端的发送距离，决定是否重新确认（进度未变时）。
// - 当接收到END包或路径超时时，申请中间缺失包的重发。
//
// 接收项：
// - Seq  	响应数据序列号
// - Dist 	发送距离
// - Data 	响应数据
// - END 	END包标记（发送完成）
// - BYE 	BYE信息（结束）
//
type recvServ struct {
	ID    uint16         // 数据ID#RCV（<=ID#SND）
	AReq  chan<- *ackReq // 确认申请（-> xSender）
	Rtp   <-chan uint32  // 请求重发通知
	Acks  <-chan ackDst  // 确认通知（数据消耗|再次确认）
	seq   uint32         // 当前序列号
	alive time.Time      // 活着时间戳

	// 资源请求发送就绪后超时
	// 用于发送服务通知衔接（接收准备）。
	readyOut <-chan time.Time
}

func newRecvServ(id uint16, x *forAcks) *recvServ {
	return &recvServ{
		ID:     id,
		AckReq: x.AckReq,
		Rtp:    x.Rtp,
		Rack:   x.Rack,
		Ack:    x.Ack,
		seq:    xLimit32,
		// zero
		alive: time.Time{},
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
// 资源请求发送就绪调用（servSend）。
// 用于接收器准备接收首个响应数据片的超时重发请求。
// 如果已经接收到响应数据，则无行为。
//
// seq 为对端响应的首个分组的序列号。
// 注：也即资源请求的最后一个分组的确认号（预设约定）。
//
func (r *recvServ) Ready(seq uint32) {
	if !r.alive.IsZero() {
		return
	}
	r.seq = seq
	r.readyOut = time.After(ReadyTime)
}

//
// 启动监听。
// 接收各个评估模块的信息，构造确认申请传递给发送总管。
//
func (r *recvServ) Listen(xs *xSender, stop *goes.Stop) {
	// 接收历史栈
	// 每个分组的数据长度累计值存储。
	buf := make(map[int]int64)

	for {
		var rtp, ack int

		select {
		case <-stop.C:
			return
		case rtp = <-r.Rtp:
		case ack = <-r.AckEval():
		}
	}
}

//
// 启动接收服务。
// 接收对端传送来的响应数据及相关信息。
// 分解信息派发给各个评估模块。
//
func (r *recvServ) Serve(rp chan<- *rtpInfo, stop *goes.Stop) {
	//
}

//
// 接收响应数据。
//
func (r *recvServ) Receive(seq, rpz int, data []byte) {
	//
}

//
// 响应数据关联信息。
// 从对端发送过来的响应信息中提取的信息。
// 用于应用接收器消耗数据并提供进度控制。
//
type sndInfo struct {
	Seq      uint32 // 序列号
	Data     []byte // 响应数据
	Beg, End bool   // 首位数据报标记
}

//
// 确认评估器。
// - 由应用接收器对数据的消耗，发送进度确认。
// - 评估发送距离，低优先级重新发送确认。
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
// ach 为确认号递送信道。
//
func (a *ackEval) Serve(ach chan<- ackDst, exit *goes.Stop) {
	// BYE等待超时
	var byeOut <-chan time.Time

	// END重复确认
	wait := RtpEndTime

	// 无效进度计数
	var acnt int

	for {
		select {
		// 通常由上层收到BYE后触发
		case <-exit.C:
			return

		// END重复确认
		case <-byeOut:
			if wait > ByeTimeout {
				log.Println("wait BYE message timeout.")
				return
			}
			ach <- ackDst{a.endx, 0}
			wait += wait
			byeOut = time.After(wait)

		// BYE信息并不传递至此，
		// 会由上层直接结束（exit）。
		case si := <-a.Snd:
			a.update(si)
			if !a.beg {
				continue
			}
			ack := a.pool.Acked()
			bs, done := a.pool.ToAck(ack)

			switch {
			case bs != nil:
				a.App.Puts(bs)
				ach <- ackDst{ack, a.pool.Dist()}
				acnt = 0

			case acnt > reAckLimit:
				ach <- ackDst{ack, a.pool.Dist()}
				acnt++
			}
			// 最后设置
			if done {
				byeOut = time.After(wait)
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
	acks *roundOrder       // 确认号有序集
	pool map[uint32][]byte // 数据存储（key:ack）
	end  bool              // 已接收到END包
}

func newRecvPool() *recvPool {
	return &recvPool{pool: make(map[uint32][]byte)}
}

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
// 移动到确认号。
// 返回确认号之前的连续数据片和是否已全部接收完。。
// ack 为对应确认号的数据片序列号。
// 注：
// 会清除确认号之前的历史数据。
//
func (p *recvPool) ToAck(ack uint32) ([][]byte, bool) {
	var bs [][]byte
	buf := p.acks.Queue()

	for i := 0; i < p.acks.Clean(ack); i++ {
		ack = buf[i]
		bs = append(bs, p.pool[ack])
		delete(p.pool, ack)
	}
	return bs, p.end && p.acks.Size() == 0
}

//
// 确认检查。
// 检查并返回当前进度的确认号。
// 注记：
// 前一个数据的确认号与后一个数据的序列号相等即为连续。
//
func (p *recvPool) Acked() uint32 {
	buf := p.acks.Queue()
	ack := p.acks.Beg()

	for i := 0; i < len(buf); i++ {
		a2 := buf[i]
		if ack != roundBegin(a2, len(p.pool[a2])) {
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
func (p *recvPool) Dist() uint {
	if p.acks == nil {
		return uint(len(p.pool))
	}
	return uint(p.acks.Size())
}

//
// 是否已准备好。
// 即已经收到过初始分组的序列号。
//
func (p *recvPool) Ready() bool {
	return p.acks != nil
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
// ack 为初始包（BEG）的序列号。
//
func newRtpEval(ri <-chan *rtpInfo) *rtpEval {
	return &rtpEval{
		Info: ri,
		cur:  time.Now(),
	}
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
// 会重置路径超时为正常的包间隔。
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
