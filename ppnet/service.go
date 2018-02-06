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
/// - 根据传输路径超时（连续包间隔时间的2-3倍），评估所需包丢包并请求重发。
/// - 如果收到END包，主动持续请求确认号的目标包（结合新到包）。
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
)

// 基础常量设置。
const (
	AliveProbes = 6                 // 保活探测次数上限
	AliveTime   = 120 * time.Second // 保活时间界限，考虑NAT会话存活时间
	AliveIntvl  = 10 * time.Second  // 保活报文间隔时间
	CalmExpire  = 90 * time.Second  // 服务静置期限（SND/RCV子服务无活动）
	ReadyTime   = 6 * time.Second   // 请求发送就绪，接收超时
)

// 基本参数常量
const (
	xLimit32     = 1<<32 - 1 // 序列号上限（不含）
	xLimit16     = 1<<16 - 1 // 数据体ID上限
	recvTimeoutx = 2.5       // 接收超时的包间隔倍数
	recvLossx    = 12        // 接收丢包评估（数量距离积）
)

// END关联全局变量。
// 可能根据网络状态而调整为适当的值，但通常无需修改。
// （非并发安全）
var (
	EndAcks = 3 // END重复确认次数上限
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
	Dist int    // 确认距离（0值无漏包）
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

//
// 接收服务器。
// 处理对端传输来的响应数据。向发送总管提供确认或重发申请。
// 只有头部信息的申请配置，没有负载数据传递。
// 一个数据体对应一个本类实例。
//
// - 根据应用接收数据的情况确定进度号，约束对端发送速率。
// - 评估中间空缺的序号，决定是否申请重发。
// - 当接收到END包时，优先申请中间缺失包的重发。
// - 评估对端的发送距离，决定是否重新确认（进度未变时）。
// - 处理发送方的重置接收指令，重新接收数据。
//
// 重置接收：
// 发送方的重置接收点可能处于应用接收器已消化的数据段，
// 因此需要计算截除该部分的数据。故需记录长度累计历史。
//
// 接收项：
// - Seq  	响应数据序列号
// - Dist 	发送距离
// - Data 	响应数据
// - RST 	重置接收标记
// - RPZ-Size 	扩展重组大小
// - END 	END包标记（发送完成）
// - BYE 	BYE信息（结束）
//
type recvServ struct {
	ID       uint16           // 数据ID#RCV（<=ID#SND）
	AReq     chan<- *ackReq   // 确认申请（-> xSender）
	Rtp      <-chan int       // 请求重发通知
	Rack     <-chan int       // 再次确认通知
	Ack      <-chan int       // 应用确认通知（数据消耗）
	alive    time.Time        // 活着时间戳
	readyOut <-chan time.Time // 资源请求发送就绪后超时
}

func newRecvServ(id uint16, x *forAcks) *recvServ {
	return &recvServ{
		ID:     id,
		AckReq: x.AckReq,
		Rtp:    x.Rtp,
		Rack:   x.Rack,
		Ack:    x.Ack,
		// alive:  time.Time{}, // zero
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
func (r *recvServ) Ready() {
	if !r.alive.IsZero() {
		return
	}
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
func (r *recvServ) Serve(rp chan<- *rtpInfo, ai chan<- *ackInfo, stop *goes.Stop) {
	//
}

//
// 接收响应数据。
//
func (r *recvServ) Receive(seq, rpz int, data []byte) {
	//
}

//
// 确认关联信息。
// 发送方对本端确认的回馈（确认的确认）。
//
type ackInfo struct {
	Seq  int  // 响应序列号
	Dist int  // 发送距离
	End  bool // 传输完成
}

//
// 再确认评估。
// 条件：
// - 发送距离太大，且进度停滞；
// - 发送方的确认号低于当前进度线较多；
// - 等待接收新的数据报超时（END包之前）；
// - 等待BYE包超时（已全部接收）。
//   重复END包确认，最多EndAcks次之后结束服务。
// 注：
// 如果进度前移，新发出的确认携带了进度信息，故无需重发确认。
// 本重发通常仅在数据体的末尾分组部分才会用到（已无新的数据报抵达）。
//
type reAck struct {
	Ack <-chan *ackInfo // 确认信息信道
}

//
// 响应数据关联信息。
// 从对端发送过来的响应信息中提取的信息。
// 用于应用接收器消耗数据并提供进度控制。
//
type sndInfo struct {
	Seq  uint32 // 序列号
	Data []byte // 响应数据
	End  bool   // 传输完成
	Bye  bool   // 发送结束/退出
}

//
// 返回对应的确认号。
//
func (s *sndInfo) Ack() uint32 {
	return roundPlus(s.Seq, len(s.Data))
}

//
// 确认评估器。
// 应用接收器消化数据，制约当前的确认进度。
//
type ackEval struct {
	sch  <-chan *sndInfo // 响应数据递送
	Pool *recvPool       // 数据接收管理
	App  *appReceive     // 应用接收器
}

//
// 启动一个确认评估服务。
// ach 为确认号递送信道。
//
func (a *ackEval) Serve(ach chan<- uint32, exit *goes.Stop) {
	// 重置后，已消耗数量
	var used int64

	for {
		select {
		case <-exit.C:
			return
		case sd := <-a.Snd:
			switch {
			case sd.Rpz > 0: // and RST
				used = a.Pool.RpzReset(sd.Seq, sd.Rpz)
			case sd.Rst:
				usde = a.Pool.Reset(sd.Seq)
			}
			if used >= len(sd.Data) {
				used -= len(sd.Data)
				break
			}
			dch <- a.pack(sd.Data[used:], sd.Seq, sd.End)
			used = 0
		}
	}
}

//
// 接收队列。
// 处理接收到的数据不连续的问题。
//
type recvPool struct {
	sQue *seqQueue         // 序列号有序队列
	pool map[uint32][]byte // 数据存储（key:seq）
}

func newRecvQueue() *recvPool {
	return &recvPool{
		pool: make(map[uint32][]byte),
	}
}

//
// 接收到首个分组（BEG）。
// 注：sQue为延迟创建。
//
func (p *recvPool) First(seq uint32, b []byte) {
	p.sQue = newSeqQueue(seq)
	// 可能之前添加
	for s := range p.pool {
		p.sQue.Push(s)
	}
	p.pool[seq] = b
}

//
// 压入新的序列号及其数据。
//
func (p *recvPool) Add(seq, b []byte) {
	if p.sQue != nil {
		p.sQue.Push(seq)
	}
	p.pool[seq] = b
}

//
// 移动到确认号。
// 返回确认号之前的连续数据片（不含确认关联数据）。
// 注：会清除确认号之前的历史数据。
//
func (p *recvPool) ToAck(seq uint32) [][]byte {
	var bs [][]byte
	buf := p.sQue.Queue()

	for i := 0; i < p.sQue.Clean(seq); i++ {
		seq = buf[i]
		bs = append(bs, p.pool[seq])
		delete(p.pool, seq)
	}
	return bs
}

//
// 确认检查。
// 检查并返回确认号所确认数据的序列号。
// 注记：
// 前一个数据的确认号与后一个数据的序列号相等即为连续。
//
func (p *recvPool) AckedSeq() uint32 {
	buf := p.sQue.Queue()
	seq := buf[0]
	ack := roundPlus(seq, len(p.pool[seq]))

	for i := 1; i < len(buf); i++ {
		s2 := buf[i]
		if ack != s2 {
			break
		}
		seq = s2
		ack = roundPlus(s2, len(p.pool[s2]))
	}
	return seq
}

//
// 接收计数与确认距离。
// 外部应当先调用ToAck后获得确认距离。
// 收到起始分组之前，距离即收到的计数（1+）。
//
func (p *recvPool) Dist() uint {
	if p.sQue == nil {
		return uint(len(p.pool))
	}
	return uint(p.sQue.Size() - 1)
}

//
// 是否已准备好。
// 即已经收到过初始分组的序列号。
//
func (p *recvPool) Ready() bool {
	return p.sQue != nil
}

//
// 客户端接收处理。
// 分离为一个单独的处理类，用于写入阻塞回馈。
//
type appReceive struct {
	Recv  Receiver // 接收器实例
	total int64    // 已写入数据量累计
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
// 从对端发送过来的响应中提取的信息。
//
type rtpInfo struct {
	Seq int  // 序列号
	End bool // 传输完成（END）
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
	Rcpl *recvPool       // 数据池实例（信息共享）
	Info <-chan *rtpInfo // 基本响应信息通道
	end  bool            // 已收到END包
	last time.Time       // 上一个包的接收时间
}

//
// 创建一个重发申请评估器。
// ack 为初始包（BEG）的序列号。
//
func newRtpEval(ri <-chan *rtpInfo, rp *recvPool) *rtpEval {
	return &rtpEval{
		Rcpl: rp,
		Info: ri,
	}
}

//
// 执行评估服务。
// rch 为请求重发数据报的序列号通知信道。
//
func (r *rtpEval) Serve(rch chan<- int, stop *goes.Stop) {
	for {
		select {
		case <-stop.C:
			return

		// 末尾分组的超时机制
		case <-time.After(r.timeOut()):
			r.lossRtp(rch)

		case ri := <-r.Info:
			d := roundSpacing(r.acked, seq)
			if ri.End {
				r.end = true
			}
			// 综合评估
			if !r.lost(d) {
				if r.end {
					close(rch)
				}
				break
			}
			r.lossRtp(rch)
		}
	}
}

//
// 计算超时时间。
// 用与前一个包的时间间隔的倍数计算超时（通常2-3倍）。
// 注记：
// 每次循环（接收到一个发送信息）都会更新一次。
//
func (r *rtpEval) timeOut() time.Duration {
	tm := r.last
	r.last = time.Now()

	return time.Duration(float64(r.last.Sub(tm)) * recvTimeoutx)
}

//
// 通知重发请求的序列号。
//
func (r *rtpEval) lossRtp(rch chan<- int) {
	if lost := r.Rcpl.Lost(); lost < xLimit32 {
		rch <- lost
	}
}

//
// 是否判断为丢包（请求重发）。
// 规则：
// 1. 与进度线的距离和丢包数的乘积不大于某个限度。
//    配置变量为recvLossx，如：12允许2个丢包但距离不超过6。
// 2. 如果已经接收到END包，则优先请求重发。
//
func (r *rtpEval) lost(seq int) bool {
	d := r.Rcpl.Distance(seq)
	cnt = r.Rcpl.LossCount(d)

	if cnt == 0 {
		return false
	}
	if r.end {
		return true
	}
	return d > 1 && d*cnt > recvLossx
}
