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
///   > 如果消化处于饥饿状态，则ACK为全发送模式，即每收到一个数据报回馈一个确认。
///   > 如果消化处于忙状态，则停止发送ACK确认，缓存积累已收到的数据报。
///
/// - 根据发送距离因子，评估本端发送的ACK丢失情况，可能重新确认丢失的ACK。
/// - 根据接收到序列号的连贯性，评估中间缺失包是否丢失，主动请求重发。
/// - 根据传输路径超时（连续包间隔时间的2-3倍），评估新包丢包并请求重发。
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
	"sync"
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
	errFinish    = errors.New("all data write finish")
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
func (r *recvServ) Serve(di chan<- *dataInfo, rp chan<- *rtpInfo, ai chan<- *ackInfo, stop *goes.Stop) {
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
	Seq  int    // 序列号
	Data []byte // 响应数据
	End  bool   // 传输完成
	Bye  bool   // 发送结束/退出
}

//
// 数据信息。
// 用于客户端接收器接收有效的数据（不含重置重叠段）。
//
type dataInfo struct {
	Seq  int    // 序列号
	Data []byte // 输出数据
	End  bool   // 传输完成
}

type rstInfo struct {
	Seq int // 序列号
	Rpz int // 重组扩展大小
}

//
// 确认评估器。
// 应用接收器消化数据，制约当前的确认进度。
//
type ackEval struct {
	Snd  <-chan *sndInfo // 发送数据信道
	Pool *recvPool       // 数据池管理器
}

//
// 启动一个确认评估服务。
// dch 为向数据池管理器（recvPool）输出的信道。
// 会正确处理重置导致的当前进度处的数据重叠，但不处理重复的分组（重发导致）。
//
// 注记：
// 重置会导致话验证码会改变，因此重置之前的旧分组会被上级排除。
//
func (a *ackEval) Serve(dch chan<- *dataInfo, stop *goes.Stop) {
	// 重置后，已消耗数量
	var used int64

	for {
		select {
		case <-stop.C:
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
// 创建数据池需要的数据包。
//
func (a *ackEval) pack(data []byte, seq int, end bool) *dataInfo {
	return &dataInfo{
		Seq:  seq,
		Data: data,
		End:  end,
	}
}

//
// 响应数据接收池。
// 处理接收到的数据不连续的问题。
// 连续的部分会提交到应用接收器（appRecv）消耗。
//
// pool 中存储的是当前未消耗的数据，可能不连续。
// used 中存放历史的回绕长度足够大，视为安全。
//
// 注记：
// 本类仅接收合法（非重叠）的数据，可能不连续。
// 上层处理重置时有效数据的切分。
//
type recvPool struct {
	Dch  <-chan *dataInfo  // 接收数据（一级，接收）
	used map[int]int64     // 已消耗历史（连续数据累计）
	pool map[int]*dataInfo // 数据存放池
	ack  int               // 当前进度号（已消耗分组号+1）
	dmax int               // 接收包与进度的最大距离
	mu   sync.Mutex        // ack/pool保护
}

//
// 启动接收服务。
// ach 为通知确认申请的信道（to recvSend）。
// out 为输出数据到应用接收器（appRecv）的信道。
//
func (r *recvPool) Serve(ach chan<- int, out chan<- *dataInfo, stop *goes.Stop) {
	for {
		select {
		case <-stop.C:
			return
		case di := <-r.Dch:
			r.mu.Lock()
			d := roundSpacing(r.ack, di.Seq)
			// 忽略过时包
			if d < -1 {
				r.mu.Unlock()
				break
			}
			// 重置
			if d == -1 {
				r.ack = roundPlus(r.ack, -1)
			}
			r.pool[di.Seq] = di
			// 连续
			if d == 0 {
				r.ack = roundPlus(r.ack, r.puts(out))
				ach <- roundPlus(r.ack, -1)
			}
			if d > r.dmax {
				r.dmax = d
			}
			r.mu.Unlock()
		}
	}
}

//
// 全重置处理。
// 外部应当在递送数据之前调用。
// 返回上层需要截除的数据量（已经被消耗）。
// 零值表示seq在进度线上或之后，正常传递。
//
func (r *recvPool) Reset(seq int) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	if roundSpacing(seq, r.ack) > 0 {
		seq = r.ack
	}
	for i := 0; i < r.dmax; i++ {
		delete(r.pool, roundPlus(seq, i))
	}
	return r.surplus(seq)
}

//
// RPZ 重置处理。
// 重组是合并小包，当前包只会更大。
// 被清理的包设置为nil，以保留其存在性标记。
// 这与从数据池中删除不同，nil视为有效，影响接收包的连续性判断。
//
// 返回上层需要截除的数据量。
// 零值表示seq在进度线上或之后，正常传递。
//
func (r *recvPool) RpzReset(seq, rpz int) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	d := roundSpacing(seq, r.ack)
	switch {
	// seq在ack之后
	case d <= 0:
		r.setNil(seq, rpz+1)

	// rpz横跨ack点
	case rpz >= d:
		r.setNil(r.ack, rpz-d+1)
	}
	// 上层据此切分有效数据片段传递
	// 如果值大于数据片长度，表示数据片全为历史，忽略。
	return r.surplus(seq)
}

//
// 返回目标序列号与进度的距离。
// 注：零距离表示序列号正好是需要的分组号。
//
func (r *recvPool) Distance(seq int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return roundSpacing(r.ack, seq)
}

//
// 统计漏掉的包。
// 注记：
// 当前序列号包肯定已经存在（非漏掉包）。
//
func (r *recvPool) LossCount(dist int) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	var cnt int

	for i := 0; i < dist; i++ {
		if _, ok := r.pool[roundPlus(r.ack, i)]; !ok {
			cnt++
		}
	}
	return cnt
}

//
// 最近的一个漏包序号。
// 如果没有漏掉的包，返回-1。
// 注记：
// pool中连续的分组会全部传递给appRecv。
//
func (r *recvPool) Lost() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.pool) == 0 {
		return -1
	}
	return r.ack
}

//
// 设置为nil。
// 与从map中移除不同，nil存在视为有效。
//
func (r *recvPool) setNil(beg, rpz int) {
	for i := 0; i < rpz; i++ {
		r.pool[roundPlus(beg, i)] = nil
	}
}

//
// 计算指定位置（重置点）应用的多余消耗。
// seq 为目标点的序列号。
// 零值表示无多余的消耗量，目标点在进度线上或之后。
//
func (r *recvPool) surplus(seq int) int64 {
	if roundSpacing(seq, r.ack) <= 0 {
		return 0
	}
	return r.passed() - r.used[roundPlus(r.ack, -1)]
}

//
// 连续输出。
// 输出数据池内连续存在的数据条目（nil值有效）。
// 返回连续的条目数（可能与buf大小不同）。
//
// 注记：外部锁定。
//
func (r *recvPool) puts(out chan<- []*dataInfo) int {
	i := r.ack
	buf := make([]*dataInfo)

	for {
		di, ok := r.pool[i]
		if !ok {
			break
		}
		buf = append(buf, di)
		// must
		delete(r.pool, i)
		r.used[i] = r.passed() + len(di.Data)

		i = roundPlus(i, 1)
	}
	out <- buf
	// 距离缩减
	r.dmax -= len(buf)

	return roundSpacing(r.ack, i)
}

//
// 返回已经消耗的数据量。
//
func (r *recvPool) passed() int64 {
	return r.used[roundPlus(r.ack, -1)]
}

//
// 客户端接收处理。
// 分离为一个单独的处理类，用于写入阻塞回馈。
//
type appRecv struct {
	Recv  Receiver           // 接收器实例
	Dch   <-chan []*dataInfo // 响应数据集通道（二级，连贯）
	total int64              // 已写入数据量累计
}

//
// 开始接收器处理。
// 当完成全部数据的写入后，通过stop主动结束相关服务。
//
func (a *appRecv) Start(stop *goes.Stop) {
	for {
		select {
		case <-stop.C:
			return

		// 连贯片输出
		case ds := <-a.Dch:
			if a.Puts(ds) != nil {
				stop.Exit()
			}
		}
	}
}

//
// 连续的分组输出。
// 如果全部写入结束或出错，返回false。
//
func (a *appRecv) Puts(ds []*dataInfo) bool {
	var err error

	for _, di := range ds {
		n, err = a.Recv.Write(di.Data)
		if di.End {
			if err = a.Recv.Close(); err == nil {
				err = errFinish
			}
		}
		a.total += n

		if err != nil {
			log.Printf("%v on receive %d bytes.", err, a.total)
			break
		}
	}
	return err
}

//
// RTP关联信息。
// 从对端发送过来的响应中提取的信息。
//
type rtpInfo struct {
	Seq int  // 序列号
	Rpz int  // 重组扩展大小
	End bool // 传输完成（END）
}

//
// 重发申请评估。
// 接收对端的发送来的基本响应信息，
// 评估中间缺失的包，决定是否申请重发。
//
// 场景：
// - 超时。前两个包的间隔的2-3倍。
// - 漏包。与缺失包数量和与进度的距离相关。
// - END包到达。优先请求中间缺失的包。
//
// 如果收到END包且没有漏掉的包，则会关闭通知信道。
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

//
// 简单工具集。
///////////////////////////////////////////////////////////////////////////////

//
// 返回序列号增量回绕值。
// 注：排除xLimit32值本身。
//
func roundPlus(x, n uint32) uint32 {
	return uint32((int64(x) + int64(n)) % xLimit32)
}

//
// 返回2字节（16位）增量回绕值。
// 注：排除xLimit16值本身。
//
func roundPlus2(x, n uint16) uint16 {
	return uint16((int(x) + int(n)) % xLimit16)
}

//
// 支持回绕的间距计算。
// 环回范围为全局常量xLimit32。
//
func roundSpacing(beg, end uint32) uint32 {
	if end >= beg {
		return end - beg
	}
	// 不计xLimit32本身
	return xLimit32 - (beg - end)
}

//
// 支持回绕的间距计算。
// 环回范围为全局常量xLimit16。
//
func roundSpacing2(beg, end uint16) int {
	if end >= beg {
		return int(end - beg)
	}
	return int(xLimit16 - (beg - end))
}

//
// 支持回绕的起点计算。
// 注意dist的值应当在uint32范围内。
//
func roundBegin(end, dist uint32) uint32 {
	return roundSpacing(dist, end)
}

//
// 限定计数器。
// 对特定键计数，到达和超过目标限度后返回真。
// 不含限度值本身（如：3，返回2个true）。
// 如果目标键改变，计数起始重置为零。
//
// max 为计数最大值（应为正值）。
// step 为递增步进值。
//
// 返回的函数用于递增和测试，参数为递增计数键。
//
func limitCounter(max, step int) func(int) bool {
	var cnt, key int

	return func(k int) bool {
		if k != key {
			key = k
			cnt = 0
		}
		cnt += step
		return cnt >= max
	}
}

//
// 距离计数器。
// 用于发送距离和确认距离的计数。
// 注记：
// 数据报距离为一个不大的计量值，因此用map简单处理。
// 计数和清理通常不在同一个Go程中，因此锁保护。
//
type distCounter struct {
	sum int64            // 当前累计值
	buf map[uint32]int64 // 序列号：数据报个数累计
	mu  sync.Mutex       // 清理保护
}

func newDistCounter() *distCounter {
	return &distCounter{
		buf: make(map[uint32]int64),
	}
}

//
// 添加一个数据报计数。
// 相同序列号的数据报不重复计数。
//
// seq 为数据报的序列号。
//
func (d *distCounter) Add(seq uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.buf[seq]; ok {
		return
	}
	d.sum++
	d.buf[seq] = d.sum
}

//
// 清理已确认历史。
// ack 为当前确认进度的序列号。
//
func (d *distCounter) Clean(ack uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()

	max, ok := d.buf[ack]
	if !ok {
		return
	}
	for s, n := range d.buf {
		if n < max {
			delete(d.buf, s)
		}
	}
}

//
// 返回距离（计数）。
//
func (d *distCounter) Dist() uint {
	d.mu.Lock()
	defer d.mu.Unlock()
	return uint(len(d.buf))
}
