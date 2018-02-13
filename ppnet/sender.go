package ppnet

/////////////////////
/// DCP 发送服务实现。
/// 流程
/// 	客户端：应用请求 -> [发送请求] >>>>>>
/// 	服务端：接收请求 -> 询问应用，从io.Reader读取 -> [发送响应] >>>>>>
/// 	客户端：接收响应 -> 写入应用
///
/// 发送方
/// ------
/// 1. 发送客户的资源请求，向对方索取目标资源数据。
/// 2. 根据对方的资源请求，从注册的响应服务应用获取响应数据发送。
///
/// - 初始阶段按基准速率匀速发送。收到第一个确认后，动态评估速率发送。
/// - 从接收端反馈的确认距离，评估是否丢包和重发。
///
///
/// 有序并发
/// - 数据体ID按应用请求的顺序递增编号，数据体的发送在一个Go程中自我管理。
/// - Go程中的数据体发送（servSend）以自己评估的动态速率发送，不等待确认。
/// - 各数据体动态评估的速率会间接作用于全局基准速率（基准线）。
/// - 起始发送的数据体其数据ID随机，数据体内初始分组的序列号也随机。
///
/// 被动重发
/// - 除了最后一个数据体有超时重发机制外，原则上发送方的重发并不积极。
/// - 数据体依赖接收端的重发申请和针对确认距离进行的丢包评估，计算重发。
/// - 重发申请适用于进度之后任一序列号，评估重发仅用于确认号（进度所需下一字节）。
/// - 接收端可通过缓发确认来抑制发送方的发送速率。
///
/// 速率评估
/// - 速率以发送间隔的时间来表达，时间越长速率则越慢。
/// - 各数据体自行评估自己的发送间隔时间，向总速率评估器传递发送距离变化量和丢包判断。
/// - 总速率评估器根据各数据体的发送距离变化量和丢包情况，评估当前实际发送速率。
/// - 总速率评估器的基准速率被定时评估调优，同时它也作为各数据体的基准速率。
/// - 每个交互期的数据量也用于基准速率调优参考。
///
/// 注：
/// 各数据体按自身评估的速率并发提供构造好的数据报，由发送总管统一发送。
///
///////////////////////////////////////////////////////////////////////////////

import (
	"bufio"
	"io"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/qchen-zh/pputil/goes"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// 基础常量设置。
const (
	BaseRate    = 10 * time.Millisecond  // 基础速率。初始默认发包间隔
	RateLimit   = 100 * time.Microsecond // 极限速率。万次/秒
	SendTimeout = 2 * time.Minute        // 发送超时上限
)

// 通道缓存（弱有序）
const (
	postChSize = 20 // 数据报递送通道缓存
	evalChSize = 6  // 速率评估距离增减量传递通道缓存
	byeChSize  = 4  // BYE消息通知信道大小
)

// 基本参数常量
const (
	capRange            = 5 * BaseRate           // 变幅容度
	lossFall            = 2 * BaseRate           // 丢包速率跌幅
	lossStep            = 4                      // 丢包一次的步进计数
	lossLimit           = 5.0                    // 丢包界限（待统计调优）
	lossRecover         = 15                     // 丢包恢复总滴答数
	lossTick            = 1 * time.Second        // 丢包恢复间隔（一个滴答）
	lossBase            = 20                     // 丢包总权衡基值（线性减速）
	baseTick            = 10 * time.Second       // 基准速率评估间隔（一个滴答）
	baseRatio           = 0.1                    // 基准速率调整步进比率
	dataFull      int64 = 1024 * 576             // 满载数据量（概略值）
	sendDistance        = 1024                   // 发送距离上限值
	sendBadTries        = 4                      // 发送出错再尝试次数
	sendBadSleep        = 100 * time.Millisecond // 发送出错再尝试暂停时间
	sendRateTotal       = 256.0                  // 子发送服务速率评估总步进值
	sendRateZoom        = 1.0                    // 子发送服务速率评估缩放率
)

//
// 总发送服务（发送总管）。
// 按自身评估的速率执行实际的网络发送。
//
// 通过缓存信道获取基本有序的各数据体递送的数据报。
// 提取各接收服务器的ACK申请，设置确认，尽量携带数据发送。
// 各数据体的递送由自身速率决定，无顺序关系，仅由信道缓存获得一个简单顺序。
//
// 一个4元组两端连系对应一个本类实例。
//
type xSender struct {
	Conn  *connWriter    // 数据报网络发送器
	Post  <-chan *packet // 待发送数据包通道（有缓存，一定有序性）
	Acks  <-chan *ackReq // 待确认信息包通道（同上）
	Bye   <-chan ackBye  // BYE通知信道
	Eval  *rateEval      // 速率评估器
	Dcps  *dcps          // 子服务管理器
	Exit  *goes.Stop     // 外部结束通知
	curid int            // 当前数据体ID存储
}

func newXSender(w *connWriter, dx *dcps, re *rateEval, exit *goes.Stop) *xSender {
	return &xSender{
		Conn:  w,
		Post:  dx.Post,
		Acks:  dx.AckReq,
		Bye:   dx.Bye,
		Eval:  re,
		Dcps:  dx,
		Exit:  exit,
		curid: xLimit16,
	}
}

//
// 启动发送服务。
//
func (x *xSender) Serve() {
	for {
		var p *packet
		var ack *ackReq

		select {
		case <-x.Exit.C:
			return

		case p = <-x.Post:
			// 忽略响应出错
			// 接收端可等待超时后重新请求。
			if p == nil {
				continue
			}
			ack = x.AckReq()

		case ack = <-x.Acks:
			if ack == nil {
				continue
			}
			p = x.Packet()

		case bye := <-x.Bye:
			p = x.Packet()
			x.SetBye(p, bye)
			// clean..
			x.Dcps.Done(bye.ID, bye.Req)
		}

		if ack != nil {
			x.SetAcks(p, ack)
		}
		// 失败后退出
		if x.send(p) != nil {
			log.Printf("write to net all failed.")
			return
		}
		time.Sleep(x.Eval.Rate())
	}
}

//
// 资源请求。
// 请求标识res没有大小限制，但通常仅为一个分组大小。
// 注记：
// 即便仅有一个分组，也需要发送子服务的逻辑（ACK确认与丢包重发）。
//
func (x *xSender) Request(res []byte, rc Receiver) error {
	ss, err := x.Dcps.NewRequest(res)
	if err != nil {
		return err
	}
	go ss.Serve(
		x.Eval,
		x.Exit,
		func(id uint16, ack uint32) { x.reqDone(id, ack, rc) },
	)
	return nil
}

//
// 目标id数据体的请求发送完毕。
//
func (x *xSender) reqDone(id uint16, seq uint32, rc Receiver) {
	x.Dcps.Done(id, true)

	// 开启响应接收。
	rs := x.Dcps.RspRecvServ(id, seq, rc)

	go rs.Serve(
		x.Exit,
		func(id uint16) { x.Dcps.Clean(id, false) },
	)
}

//
// 从通道提取数据报。
// 非阻塞，如果通道无数据返回一个空包。
//
func (x *xSender) Packet() *packet {
	select {
	case p := <-x.Post:
		return p
	default:
		return x.emptyPacket()
	}
}

//
// 从通道提取确认申请信息。
// 非阻塞，如果通道无数据返回nil。
//
func (x *xSender) AckReq() *ackReq {
	select {
	case r := <-x.Acks:
		return r
	default:
		return nil
	}
}

//
// 创建一个空数据报。
// 通常用于发送一个单独的确认（无携带数据）。
//
func (x *xSender) emptyPacket() *packet {
	h := header{
		SID: xLimit16,
		Seq: xLimit32,
	}
	return &packet{&h, nil}
}

//
// 发送数据报。
// 按自身的速率（休眠间隔）发送数据报，
// 如果失败会适当多次尝试，依然返回错误时，外部通常应结束服务。
// 返回的整数值为失败尝试的次数。
//
func (x *xSender) send(p *packet) error {
	var err error

	for i := 0; i < sendBadTries; i++ {
		if _, err = x.Conn.Send(*p); err == nil {
			return nil
		}
		log.Printf("send [%d:%d] packet: %v\n", p.SID, p.Seq, err)
		time.Sleep(sendBadSleep)
	}
	return err
}

//
// 设置确认信息到数据报头。
// 包含接收数据ID、接收号、确认距离或重发申请。
//
func (x *xSender) SetAcks(p *packet, req *ackReq) {
	if req.Rtp {
		p.Set(RTP)
	}
	if req.Dist > 0 {
		p.Dist = req.Dist
	}
	if req.Req {
		p.Set(QER)
	}
	p.RID = req.ID
	p.Ack = req.Ack
}

//
// 设置BYE通知（作为ACK）。
//
func (x *xSender) SetBye(p *packet, bye ackBye) {
	if bye.Req {
		p.Set(QER)
	}
	p.Set(BYE)
	p.RID = bye.ID
	// 非零，无意义
	p.Ack = rand.Uint32()
}

//
// 发送子服务上传信息通道。
// 用于创建 servSend 实例时的成员赋值。
//
type forSend struct {
	Post   chan *packet // 数据报递送信道
	Bye    chan ackBye  // 结束通知（BYE）
	Dist   chan int     // 发送距离增减量通知（-> rateEval）
	AckReq chan *ackReq // 确认申请（recvServ -> xSender）
}

func newForSend() *forSend {
	return &forSend{
		Post:   make(chan *packet, postChSize),
		Bye:    make(chan ackBye, byeChSize),
		Dist:   make(chan int, evalChSize),
		AckReq: make(chan *ackReq, postChSize),
	}
}

//
// 发送BYE通知。
//
type ackBye struct {
	ID  uint16 // 数据ID#SND（=>#RCV）
	Ack uint32 // 确认号随机
	Req bool   // 为资源请求类型
}

//
// 速率评估相关
///////////////////////////////////////////////////////////////////////////////

//
// 总速率评估。
// 为全局总发送器提供发送速率依据。
// 发送数据量影响基准速率，各数据体发送距离的增减量影响当前速率。
//
// 距离：
// 各数据体的发送距离增减量累计，越小说明整体效率越好。
// 但距离增减量累计不能体现初始的发送距离。
//
// 丢包：
// 各数据体评估丢包后会发送一个通知，用于总发送器评估调整当前速率。
// 这通常是由于初始发送的速率就偏高。
//
// 数据量：
// - 数据量越小，基准速率向慢趋稳。降低丢包评估重要性。
// - 数据量越大，基准速率趋快愈急。丢包评估更准确。
//
// 自调优：
// 根据长时间的当前速率评估计算基准速率，调优自适应。
//
type rateEval struct {
	Loss     chan struct{} // 丢包通知
	base     float64       // 基准速率
	rate     float64       // 当前速率
	mu       sync.Mutex    // base并发安全
	dataSize int64         // 数据量
	ease     easeRate      // 速率生成器
	dist     <-chan int    // 发送距离增减量（负为减）
}

//
// 新建一个评估器。
// 其生命期通常与一个对端连系相关联，连系断开才结束。
//
// total 是一个待调优的估计值，可能取1.5倍个体上限（255）。
// 该值越大，随着距离增大的速率曲线越平缓。
//
// dch 与所有数据体发送实例共享（一对连系），收集相关信息。
// 通常为一个有缓存大小的信道。
//
func newRateEval(total float64, dch <-chan int) *rateEval {
	return &rateEval{
		Loss: make(chan struct{}),
		base: float64(BaseRate),
		rate: float64(BaseRate),
		ease: newEaseRate(total),
		dist: dch,
	}
}

//
// 获取基准速率值。
// 主要供各数据体的发送进程参考。
//
func (r *rateEval) Base() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.base
}

//
// 获取当前的发送速率。
//
func (r *rateEval) Rate() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return time.Duration(r.rate)
}

//
// 启动评估服务。
// 包含距离增减量因子和丢包信息。
// 评估是一个持续的过程，由外部读取当前评估结果。
//
// 可通过返回的stop中途中断评估服务（然后以不同的zoom重启）。
// 注意最好只有一个服务在运行。
//
// 数据量对基准速率的影响由外部调用产生。
//
func (r *rateEval) Serve(zoom float64, stop *goes.Stop) {
	go r.lossEval(stop)
	go r.distEval(zoom, stop)
	go r.baseEval(stop)
}

//
// 基准速率评估（阻塞）。
// 按一定的节奏用参考前速率调优基准速率。
//
func (r *rateEval) baseEval(stop *goes.Stop) {
	tick := time.NewTicker(baseTick)
	davg := 0.0
Loop:
	for {
		select {
		case <-stop.C:
			break Loop
		case <-tick.C:
			r.mu.Lock()
			davg += r.rate - r.base
			davg /= 2
			r.base = r.baseRate(davg, r.base)
			r.mu.Unlock()
		}
	}
	tick.Stop()
}

//
// 评估基准速率。
// - 如果当前速率与基准速率差不多或更高，适量提速。
// - 如果当前速率多数情况下低于基准速率，适量减速。
// 注：
// 最终基准速率不可高于全局设定的极限速率（RateLimit）。
//
// avg 为两个速率差的积累平均值。负值加速，正值减速。
//
func (r *rateEval) baseRate(avg, base float64) float64 {
	v := base + avg/base*baseRatio

	if v < float64(RateLimit) {
		return float64(RateLimit)
	}
	return v
}

//
// 评估丢包影响。
// 随着丢包次数越多，当前速率增量下降。
// 但随着时间移动，当前速率会慢慢恢复。
//
// 下降为线性增量，恢复为渐近式向基线靠拢。
//
func (r *rateEval) lossEval(stop *goes.Stop) {
	var x int // 加速参量
	var y int // 减速参量
	// 恢复滴答
	tick := time.NewTicker(lossTick)
Loop:
	for {
		select {
		case <-stop.C:
			break Loop // 退出
		case <-r.Loss:
			if x > 0 {
				x-- // 恢复累积
			}
			// 无限递增
			y++
			r.mu.Lock()
			r.rate += r.base * (float64(y) / lossBase)
			r.mu.Unlock()
		case <-tick.C:
			if y > 0 {
				y-- // 减速消解
			}
			if x > lossRecover {
				break // 停止加速
			}
			x++
			r.mu.Lock()
			r.rate = r.recover(float64(x), lossRecover)
			r.mu.Unlock()
		}
	}
	tick.Stop()
}

//
// 恢复，逐渐向基线靠拢。
// x 值为当前滴答数。
// d 值为全局设置 lossRecover。
//
func (r *rateEval) recover(x, d float64) float64 {
	if x >= d {
		return r.base
	}
	return r.rate - x/d*(r.rate-r.base)
}

//
// 评估距离增减量影响（当前速率）。
// 参考各数据体发送来的发送距离增减量评估当前速率。
// 增减量累积为负时，会提升当前速率。
//
// 各数据体在自我休眠（发送间隔）前应当先传递距离数据。
//
func (r *rateEval) distEval(zoom float64, stop *goes.Stop) {
	var d float64
	for {
		select {
		case <-stop.C:
			return
		case v := <-r.dist:
			d += float64(v)
		}
		r.mu.Lock()
		r.rate += r.ease.Send(d, r.base, float64(capRange), zoom)
		r.mu.Unlock()
	}
}

//
// 数据量影响。
// 当达到最大值之后即保持不变，数据量只会增加不会减小。
// 数据量因子表达的是起始传输的持续性，故无减小逻辑。
//
// 通常，每发送一个数据报就会调用一次。size应当非负。
// 并发安全。
//
func (r *rateEval) Flow(size int64) {
	if r.dataSize >= dataFull {
		return
	}
	r.dataSize += size
	if r.dataSize > dataFull {
		r.dataSize = dataFull
	}
	r.mu.Lock()
	r.base += r.ratioFlow(r.dataSize) * float64(BaseRate)
	r.mu.Unlock()
}

//
// 数据量速率评估。
// 返回一个基础速率的增减比率（-0.4 ~ 0.6）。
// 共分100个等级，数据量越大，返回值越小。
// 注：
// [-0.4~0.6] 是一个直觉估值，待测试调优。
// 大概情形为：如果初始数据就较大，则趋快（先减），否则趋慢。
//
func (r *rateEval) ratioFlow(amount int64) float64 {
	if amount >= dataFull {
		return -0.4
	}
	return 0.6 - float64(amount*100/dataFull)/100
}

//
// 单元数据流。
// 对应单个的数据体，缓存即将发送的数据。
// 提供数据量大小用于速率评估。
//
type flower struct {
	buf  *bufio.Reader // 数据源
	size int64         // 剩余数据量
}

func newFlower(r io.Reader, size int64) *flower {
	return &flower{bufio.NewReader(r), size}
}

//
// 切分目标大小的分组。
// 返回的切片大小可能小于目标大小（数据已经读取完）。
//
func (f *flower) Piece(size int) ([]byte, error) {
	b := make([]byte, size)
	n, err := f.buf.Read(b)
	f.size -= int64(n)

	if err != nil {
		f.size = 0
	}
	return b[:n], err
}

//
// 返回数据量剩余大小。
// 容错初始大小设置错误的情况。
//
func (f *flower) Size() int64 {
	if f.size < 0 {
		return 0
	}
	return f.size
}

//
// 评估速率。
// 参考当前动态基准速率计算发送速率，用于外部发送休眠。
// 包含普通状态和丢包状态下的速率获取。
// 每一个数据体发送实例包含一个本类实例。
//
// 距离：
// - 发送距离越大，是对方确认慢（应用局限），故减速。
// - 确认距离越大，丢包概率越大（网络拥塞），故减速。
//
// 注：
// 对外传递发送距离增减量，用于总速率评估器（rateEval）参考。
// 不含初始段的匀速（无评估参考）。
//
type evalRate struct {
	*lossEval            // 丢包评估器
	Zoom      float64    // 发送曲线缩放因子
	Ease      easeRate   // 曲线速率生成
	Dist      chan<- int // 发送距离增减量通知
	prev      int        // 前一个发送距离暂存
}

func newEvalRate(le *lossEval, zoom float64, er easeRate, dch chan<- int) *evalRate {
	return &evalRate{
		lossEval: le,
		Zoom:     zoom,
		Ease:     er,
		Dist:     dch,
	}
}

//
// 计算发送速率。
// 对外传递当前发送距离增减量。评估即时速率。
// d 为发送距离（1-1023）。
// base 为当前动态基准速率。
//
// 每发送一个数据报调用一次。
//
func (e *evalRate) Rate(d int, base float64) time.Duration {
	if e.prev == 0 {
		e.prev = d
	}
	// 可能阻塞（很难）
	// 传递相对增减量更合适。
	e.Dist <- int(d - e.prev)
	e.prev = d

	if e.Lost() {
		return e.lossEval.Rate(float64(d), base)
	}
	return time.Duration(
		e.Ease.Send(float64(d), base, e.Zoom, float64(capRange)),
	)
}

//
// 丢包评估器（发送方）。
// 根据确认距离评估是否丢包，提供丢包阶段速率。
//
type lossEval struct {
	Zoom float64    // 丢包曲线缩放因子
	Ease easeRate   // 曲线速率生成
	fall float64    // 丢包跌幅累加
	lost int        // 丢包计步累加
	mu   sync.Mutex // 评估&查询分离
}

//
// 新建一个实例。
// 丢包跌幅累计初始化为基础变幅容度。
//
func newLossEval(ease easeRate, zoom float64) *lossEval {
	return &lossEval{
		Ease: ease,
		Zoom: zoom,
		fall: float64(capRange),
	}
}

//
// 是否为丢包模式。
//
func (l *lossEval) Lost() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lost > 0
}

//
// 丢包评估。
// 确认距离越大，丢包概率越大。
// 速率越低，容忍的确认距离越小，反之越大。
// d 为确认距离。
// base 为当前动态基准速率。
//
// 每收到一个确认调用一次。
// 返回true表示一次丢包发生。
//
func (l *lossEval) Eval(d, base float64) bool {
	// 速率容忍
	v := base / float64(BaseRate)
	// 距离关系
	if d*v < lossLimit {
		return false
	}
	l.mu.Lock()
	l.lost += lossStep
	l.fall += float64(lossFall)
	l.mu.Unlock()

	return true
}

//
// 丢包阶段速率。
// 丢包之后的速率为快减方式：先快后缓。
// d 为发送距离。
//
func (l *lossEval) Rate(d, base float64) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.lost > 0 {
		l.lost--
	}
	if l.lost <= 0 {
		l.fall = float64(capRange)
	}
	return time.Duration(l.Ease.Loss(d, base, l.fall, l.Zoom))
}

//
// 缓动速率。
// 算法：Y(x) = Extent * Fn(x) + base
//
// X：横坐标为预发送数据报个数。
// Y：纵坐标为发包间隔时间（越长则越慢）。
//
type easeRate struct {
	ratioCubic
}

//
// 新建一个速率生成器。
// total 为总距离，是速率曲线的基础值。
//
func newEaseRate(total float64) easeRate {
	return easeRate{ratioCubic{total}}
}

//
// 正常发送速率。
// 慢进：先慢后快（指数进度）。
//
// x 为变化因子（可能为负），不局限在底部总值内。
// base 为基准速率（间隔时长）。
// cap 为速率降低的幅度容量。
// zoom 为缩放因子（比率曲线中的 k）。
//
func (s easeRate) Send(x, base, cap, zoom float64) float64 {
	return base + s.UpIn(x, zoom)*cap
}

//
// 丢包后发送速率。
// 快减：很快慢下来，越到后面减速越缓。
//
func (s easeRate) Loss(x, base, cap, zoom float64) float64 {
	return base + s.UpOut(x, zoom)*cap
}

//
// 比率曲线（Easing.Cubic）。
// 用于确定发送距离与发送速率之间的网络适配。
//
// 这是一种主观的尝试，曲线为立方关系（借鉴TCP-CUBIC）。
// 试图适应网络的数据传输及其社会性的拥塞逻辑。
//
type ratioCubic struct {
	Total float64 // 横坐标值总量，曲线基础
}

//
// 慢升-右下弧。
// 从下渐进向上，先慢后快，增量越来越多。
// 注：渐近线在下。
//
// x 为横坐标变量，值应该在总量（rc.Total）之内。
// k 为一个缩放因子，值取1.0上下浮动。
//
func (rc ratioCubic) UpIn(x, k float64) float64 {
	x /= rc.Total
	return x * x * x * k
}

//
// 快升-左上弧。
// 先快后慢，增量越来越少。向上抵达渐进线。
// 注：渐近线在上。
//
func (rc ratioCubic) UpOut(x, k float64) float64 {
	x = x/rc.Total - 1
	return (x*x*x + 1) * k
}
