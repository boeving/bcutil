package dcp

/////////////////////
/// DCP 内部服务实现。
/// 流程
/// 		客户端：应用请求 >> [发送]； [接收] >> 写入应用。
/// 		服务端：[接收] >> 询问应用，获取io.Reader，读取 >> [发送]。
///
/// 发送方
/// ------
/// - 根据对方的请求，从注册的响应服务应用获取响应数据。
/// - 一个交互期的初始阶段按基准速率匀速发送。收到第一个确认后，获得网络基本容量。
/// - 后续的发送附带有效的发送距离（非零。减去进度线序列号和网络容量）。
/// - 由对方发来的数据报的确认距离评估丢包情况，决定重发。
///
/// 接收端
/// ------
/// - 发送客户应用的请求，向对端索取目标资源数据。
/// - 接收对端的响应数据，写入请求时附带的接收器接口（Receiver）。
/// - 根据接收器写入的进度，决定ACK的发送，间接抑制对端响应数据的发送速率。
/// - 根据响应里携带的对端的发送距离，评估本端发送的ACK丢失情况，重新确认或更新确认。
/// - 根据接收到的序列号顺序，评估中间缺失号是否丢失，决定请求重发。
///   注：当收到END数据报时，中间缺失数据报的请求重发更优先。
///
///
/// 顺序并发
/// - 数据ID按应用请求的顺序编号，并顺序发送其首个分组。之后各数据体Go程自行发送。
/// - 数据体Go程以自身动态评估的即时速率发送，不等待确认，获得并行效果。
/// - 数据体Go程的速率评估会间接作用于全局基准速率（基准线）。
/// - 一个交互期内起始的数据体的数据ID随机产生，数据体内初始分组的序列号也随机。
///   注：
///   首个分组按顺序发送可以使得接收端有所凭借，而不是纯粹的无序。
///   这样，极小数据体（仅有单个分组）在接收端的丢包判断就容易了（否则很难）。
///
/// 有序重发
/// - 每个数据体权衡确认距离因子计算重发。
/// - 序列号顺序依然决定优先性，除非收到接收端的主动重发请求。
///
/// 乱序接收
/// - 网络IP数据报传输的特性使得顺序的逻辑丢失，因此乱序是基本前提。
/// - 每个数据体内的分组由序列号定义顺序，按此重组数据体。
/// - 发送方数据体的发送是一种并行，因此接收可以数据体为单位并行写入应用。
///   注：小数据体通常先完成。
///
/// 乱序确认
/// - 每个数据体的应用写入的性能可能不同，通过确认回复制约发送方的发送速率（ACK滞速）。
/// - 如果已经收到END包但缺失中间的包，可主动请求重发以尽快交付数据体。
/// - 丢包判断并不积极（除非已收到END包），采用超时机制：前2个包间隔的2-3倍（很短）。
///   注：尽量减轻不必要重发的网络负担。
/// - 从「发送距离」可以评估确认是否送达（ACK丢失），必要时再次确认。
///
/// 速率评估
/// - 速率以发送间隔的时间来表达，时间越长速率则越慢。
/// - 各数据体自行评估自己的发送间隔时间并休眠，向总速率评估器传递发送距离变化量和丢包判断。
/// - 总速率评估器根据各数据体的发送距离变化量和丢包情况，评估当前实际发送速率。
/// - 总速率评估器的基准速率被定时评估调优，同时它也作为各数据体的基准速率。
/// - 每个交互期的数据量也用于基准速率调优参照。
/// 注：
/// 各数据体按自身评估的速率并发提供构造好的数据报，由总发送器统一发送。
///
///////////////////////////////////////////////////////////////////////////////

import (
	"bufio"
	"bytes"
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
	BaseRate    = 10 * time.Millisecond  // 基础速率。初始默认发包间隔
	RateLimit   = 100 * time.Microsecond // 极限速率。万次/秒
	SendTimeout = 500 * time.Millisecond // 发送超时
	TimeoutMax  = 120 * time.Second      // 发送超时上限
	AliveProbes = 6                      // 保活探测次数上限
	AliveTime   = 120 * time.Second      // 保活时间界限，考虑NAT会话存活时间
	AliveIntvl  = 10 * time.Second       // 保活报文间隔时间
)

// 基本参数常量
const (
	capRange          = 5 * BaseRate     // 变幅容度
	lossFall          = 2 * BaseRate     // 丢包速率跌幅
	lossStep          = 4                // 丢包一次的步进计数
	lossLimit         = 5.0              // 丢包界限（待统计调优）
	lossRecover       = 15               // 丢包恢复总滴答数
	lossTick          = 1 * time.Second  // 丢包恢复间隔（一个滴答）
	lossBase          = 20               // 丢包总权衡基值（线性减速）
	baseTick          = 10 * time.Second // 基准速率评估间隔（一个滴答）
	baseRatio         = 0.1              // 基准速率调整步进比率
	dataFull    int64 = 1024 * 576       // 满载数据量（概略值）
	seqLimit          = 0xffff           // 序列号上限（不含）
)

var (
	errResponser = errors.New("not set Responser handler")
	errPieces    = errors.New("the pieces amount is overflow")
)

//
// service DCP底层服务。
// 一个对端4元组连系对应一个本类实例。
//
type service struct {
	Resp    Responser            // 请求响应器
	Sndx    *xSender             // 总发送器
	Pch     chan packet          // 数据报发送信道备存
	ResPool map[uint16]*servSend // 发送服务器池（key:数据ID#RCV）
	RecPool map[uint16]*recvServ // 接收服务器池（key:数据ID#SND）
	NetCap  int                  // 线路容量（数据报个数）
	clean   func(net.Addr)       // 断开清理（Listener）
}

func newService(w *connWriter, clean func(net.Addr)) *service {
	ch := make(chan packet)

	return &service{
		Sndx:    newXSender(w, ch),
		Pch:     ch,
		RecPool: make(map[uint16]*recvServ),
		NetCap:  0,
		clean:   clean,
	}
}

//
// 开始DCP服务。
//
func (s *service) Start() *service {
	//
}

//
// 递送数据报。
// 由监听器读取网络接口解析后分发。
//
// 事项：
// - 判断数据报是对端的请求还是对端对本地请求的响应。
//   请求交由响应器接口，响应交由接收器接口。
// - 向响应器传递确认距离（对本端响应的确认）。
//
func (s *service) Post(pack packet) {
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
// 会话与校验。
// 用于两个端点的当前连系认证和数据校验。
// 应用初始申请一个会话时，对端发送一个8字节随机值作为验证前缀。
// 该验证前缀由双方保存，不再在网络上传输。
// 之后的数据传输用一个固定的方式计算CRC32校验和。
// 算法：
//  CRC32(
//  	验证前缀 +
//  	数据ID #SND + 序列号 +
//  	数据ID #ACK + 确认号 +
//  	发送距离 + 确认距离 +
//  	数据
//  )
// 每一次的该值都会不一样，它既是对数据的校验，也是会话安全的认证。
// 注：
// 这仅提供了简单的安全保护，主要用于防范基于网络性能的攻击。
// 对于重要的数据，应用应当自行设计强化的安全措施。
//
type session struct {
	//
}

//
// 总发送器（发送总管）。
// 按自身评估的速率执行实际的网络发送。
//
// 通过缓存信道获取各数据体Go程的数据包。
// 根据接收服务器的要求发送ACK确认，尽量携带数据发送。
// 各Go程的递送无顺序关系，因此实现并行的效果。
//
// 外部保证每个数据体的首个分组优先递送，
// 因此高数据ID的数据体必然后发送，从而方便接收端判断是否丢包。
//
// 一个4元组两端连系对应一个本类实例。
//
// 注记：
// 用单向链表存储确认信息申请，以保持先到先用的顺序。
//
type xSender struct {
	Conn *connWriter   // 数据报网络发送器
	Rate *rateEval     // 速率评估器
	Pch  <-chan packet // 待发送数据包通道（有缓存）
	head *ackInfo      // 确认信息链头
	foot *ackInfo      // 确认信息链尾
}

func newXSender(w *connWriter, pch <-chan packet) *xSender {
	return &xSender{
		Conn: w,
		Rate: newRateEval(),
		Pch:  pch,
	}
}

//
// 发送请求。
// 由外部客户请求调用，有更高的优先级。
//
func (x *xSender) Request(res []byte) error {
	//
}

//
// 申请确认发送。
// 仅由接收服务器调用，用于正常的确认和重发申请。
//
// sid 为接收到的发送数据ID#SND（将变成#RCV）。
// seq 为确认的序列号或申请重发的序列号，将作为确认字段里的数据。
// rtp 为申请重发标识。
//
// 注：
// 正常的确认与重发申请不同仅在于重发标记和确认距离（为零）。
//
func (x *xSender) Ack(sid, seq uint16, rtp bool) {
	//
}

//
// 确认信息。
//
type ackInfo struct {
	next *ackInfo
	//
}

//
// 接收服务器。
// 处理对端传输来的响应数据，一个数据体对应一个本类实例。
//
// 调用发送服务接口，拥有高优先级。
// - 根据应用的执行情况确定进度确认号，制约对端发送速率。
// - 评估中间空缺的序号，决定是否申请重发。
// - 当接收到END包时，申请中间缺失包的重发。
// - 评估对端的发送距离，决定是否再次确认。
//
// 注：
// 只有头部信息的申请配置，没有负载数据传递。
//
type recvServ struct {
	Recv   Receiver // 接收器实例，响应写入
	Acked  uint16   // 实际确认号，接收进度
	CurAck uint16   // 当前确认号，发送抑制
	Sndx   *xSender // 总发送器，头信息申请
}

//
// 接收响应数据。
// seq  为响应数据包的序列号。
// dist 为数据包携带的对端的发送距离。
//
func (r *recvServ) Receive(data []byte, seq, dist uint16) {
	//
}

//
// 数据报发送器。
// 一个响应服务对应一个本类实例。
// - 从响应器接收数据负载，构造数据报递送到总发送器。
// - 评估对端的确认距离，判断是否丢包重发。
// - 管理递增的序列号，重构丢失包之后已发送的数据报。。
//
// 注记：
// 丢失的包不可能跨越一个序列号回绕周期，巨大的发送距离会导致发送停止。
//
type servSend struct {
	ID     uint16         // 数据ID#SND
	Seq    int            // 初始序列号
	Ack    int            // 最新的响应确认
	Netcap int            // 网络容量
	Pch    chan<- *packet // 数据报递送通道
	resp   *response      // 响应器
	stop   *goes.Stop     // 停止接收响应
	hist   map[int][]byte // 已用备存（丢包重发用）
	seqx   int            // 当前序列号
	pmtu   int            // 路径MTU大小（可变）
	mu     sync.Mutex     // pmtu/ack同步锁
}

func newSender(r io.Reader, id uint16, seq int, pch chan<- *packet) *servSend {
	s := servSend{
		ID:   id,
		Seq:  seq,
		seqx: seq,
		Pch:  pch,
		pmtu: mtuValue[MTUBase],
		resp: newResponse(r),
	}
	// 启动服务
	s.stop = s.Serve()

	return &s
}

//
// 启动发送服务。
//
func (s *servSend) Serve() *goes.Stop {
	stop := goes.NewStop()

	go func() {
		for {
			select {
			case <-stop.C:
				return
			case s.Pch <- s.Build():
			}
		}
	}()

	return stop
}

//
// 修改路径MTU大小设定。
//
func (s *servSend) SetMTU(size int) {
	s.mu.Lock()
	s.pmtu = size
	s.mu.Unlock()
}

//
// 结束响应分片的接收。
//
func (s *servSend) Close() {
	s.stop.Exit()
}

//
// 构建数据报。
// 返回nil表示读取出错，上层应当通知对端。
//
func (s *servSend) Build() *packet {
	s.mu.Lock()
	defer s.mu.Unlock()

	h := s.header()
	b, err := s.resp.Get(h.DataSize())

	if err != nil && err != io.EOF {
		log.Printf("read [%d] error: %s", s.ID, err)
		return nil
	}
	return s.packet(h, b, err == io.EOF)
}

//
// 创建一个报头。
//
func (s *servSend) header() *header {
	return &header{
		SID:    s.ID,
		Seq:    uint16(s.seqx % seqLimit),
		SndDst: uint(s.seqx - s.Ack),
		// ...
	}
}

//
// 创建一个数据报。
// end 表示数据已经读取完毕（或出错）。
//
func (s *servSend) packet(h *header, b []byte, end bool) *packet {
	//
}

//
// 重建为更小的数据片集。
// 应对MTU中途变小且丢包的情形，seq为丢失的包序号（进度线后首个丢失）。
// 首个分组（seq）需设置重置标记。
// 注记：
// 仅需处理cache内的成员，back内成员交给环回机制。
//
func (s *servSend) Rebuild(seq, size int) []*resPiece {
	r.mu.Lock()
	r.loss = true // 无条件暂停
	r.mu.Unlock()

	n := r.seqx - seq
	if n < 0 {
		n = seqLimit - n
	}
	buf := make([]*resPiece, 0, n)

	for i := seq; ; i++ {
		p := r.cache[i]
		if p == nil {
			break
		}
		buf = append(buf, p)
	}
	buf = append(buf, r.back...)

	return r.Pieces(buf, seq, size)
}

//
// 切分数据片为指定大小的子片。
//
func pieces(data []byte, size int) [][]byte {
	n := len(data) / size
	buf := make([][]byte, 0, n+1)

	for i := 0; i < n; i++ {
		x := i * size
		buf = append(buf, data[x:x+size])
	}
	if len(data)%size > 0 {
		buf = append(buf, data[n*size:])
	}
	return buf
}

//
// 读取数据片封装。
// 便于数据片与可能的错误或io.EOF同时传递。
//
type piece struct {
	Data []byte
	Err  error
}

//
// 请求响应器。
// 提供对端请求需要的响应数据。一个数据体对应一个本类实例。
//
type response struct {
	rb   *bufio.Reader // 响应读取器
	back [][]byte      // 环回的前置分片集
}

func newResponse(r io.Reader) *response {
	return &response{
		rb: bufio.NewReader(r),
	}
}

//
// 获取下一个分片。
// 优先从back的存储从提取，不能大于指定的大小。
// back中的大数据片会被重构为指定的大小。
//
// 作为io.EOF的error保证与最后一片数据一起返回。
//
func (r *response) Get(size int) ([]byte, error) {
	if len(r.back) == 0 {
		return r.Read(size)
	}
	if len(r.back[0]) > size {
		// 重构
		r.back = pieces(bytes.Join(r.back, nil), size)
	}
	b := r.back[0]
	r.back = r.back[1:]

	return b, nil
}

//
// 从原始的流读取数据。
// 如果读取到数据尾部，io.EOF保证与最后一片数据同时返回。
// 这便于上级标注该片数据为最后一片。
//
// 如果一开始就没有可读的数据，返回的数据片为nil。
//
func (r *response) Read(size int) ([]byte, error) {
	buf := make([]byte, size)
	n, err := r.rb.Read(buf)

	if n == 0 {
		return nil, err
	}
	if err == nil {
		// 末尾探测
		_, err = r.rb.Peek(1)
	}
	return buf[:n], err
}

//
// 环回前置插入。
// 用于MTU突然变小后，已读取的数据返还重新分组。
//
func (r *response) Shift(bs [][]byte) {
	r.back = append(bs, r.back...)
}

//
// 响应分片。
// pos 有4个值，表达 Data 所处的位置：
// > 1  起始分片（BEG）
// > 0  中间段分片
// > -1 末尾分片（END）
// > 2  独立分片（BEG & END）
//
type resPiece struct {
	Seq  int    // 序列号
	Data []byte // 分片数据
	pos  int    // 分片位置
}

func (r *resPiece) Size() int {
	return len(r.Data)
}

//
// 将自身切分为指定大小的多个响应分片实例。
// 需要处理序列号的顺序递增/回绕和各分片的位置标记。
//
// seq 为起始序列号。size 为新的大小限制。
//
func (r *resPiece) Pieces(seq, size int) []*resPiece {
	if r.Size() <= size {
		return []*resPiece{r}
	}
	bs := r.pieces(r.Data, size)
	max := len(bs)
	buf := make([]*resPiece, 0, max)

	// 数据与序列号
	for i := 0; i < max; i++ {
		seq = (seq + i) % seqLimit
		buf = append(buf, &resPiece{Data: bs[i], Seq: seq})
	}
	// 位置标记
	switch {
	case r.Beg():
		buf[0].SetBeg()
	case r.End():
		buf[len(buf)-1].SetEnd()
	case r.Alone():
		buf[0].SetBeg()
		buf[len(buf)-1].SetEnd()
	}

	return buf
}

//
// 切分数据片为指定大小的子分片。
//
func (r *resPiece) pieces(data []byte, size int) [][]byte {
	var buf [][]byte
	n := len(data) / size

	for i := 0; i < n; i++ {
		x := i * size
		buf = append(buf, data[x:x+size])
	}
	if len(data)%size > 0 {
		buf = append(buf, data[n*size:])
	}
	return buf
}

func (r *resPiece) Beg() bool {
	return r.pos == 1
}

func (r *resPiece) End() bool {
	return r.pos == -1
}

func (r *resPiece) Alone() bool {
	return r.pos == 2
}

func (r *resPiece) SetBeg() {
	r.pos = 1
}

func (r *resPiece) SetEnd() {
	r.pos = -1
}

func (r *resPiece) SetAlone() {
	r.pos = 2
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
	base     float64         // 基准速率
	rate     float64         // 当前速率
	mu       sync.Mutex      // base并发安全
	dataSize int64           // 数据量
	ease     easeRate        // 速率生成器
	dist     <-chan int      // 发送距离增减量（负为减）
	loss     <-chan struct{} // 丢包通知
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
func (r *rateEval) Serve(zoom float64) *goes.Stop {
	stop := goes.NewStop()

	go r.lossEval(stop)
	go r.distEval(zoom, stop)
	go r.baseEval(stop)

	return stop
}

//
// 基准速率评估。
// 按一定的节奏评估当前速率，调优基准速率。
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
		case <-r.loss:
			if x > 0 {
				x-- // 恢复累积
			}
			// 无限递增
			y++
			r.mu.Lock()
			r.rate += r.base * (float64(y) / lossBase)
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
//
// 各数据体在自我休眠（发送间隔）前应当先传递距离数据。
// 增减量累积为负时，会提升当前速率。
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
// 注：不含初始段的匀速（无评估参考）。
// 参考全局基准速率计算自身的发送速率和休眠。
// 每一个数据体发送实例包含一个本类实例。
//
// 距离：
// - 发送距离越大，是因为对方确认慢，故减速。
// - 确认距离越大，丢包概率越大，故减速。
//
// 参考：
// 1. 基线。即全局的基准速率。
// 2. 跌幅。丢包之后的减速量（快减），拥塞响应。
//
type evalRate struct {
	*lossRate            // 丢包速率评估器
	Zoom      float64    // 发送曲线缩放因子
	Ease      easeRate   // 曲线速率生成
	Dist      chan<- int // 发送距离通知
	prev      int        // 前一个发送距离暂存
}

//
// 计算发送速率。
// 对外传递当前发送距离，评估即时速率。
// d 为发送距离（1-255）。
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
		return e.lossRate.Rate(float64(d), base)
	}
	return time.Duration(
		e.Ease.Send(float64(d), base, e.Zoom, float64(capRange)),
	)
}

//
// 丢包速率评估器。
// （发送方）
//
type lossRate struct {
	Zoom float64         // 丢包曲线缩放因子
	Ease easeRate        // 曲线速率生成
	Loss chan<- struct{} // 丢包通知信道
	fall float64         // 丢包跌幅累加
	lost int             // 丢包计步累加
	mu   sync.Mutex      // 丢包评估分离
}

//
// 新建一个实例。
// 丢包跌幅累计初始化为基础变幅容度。
//
func newLossRate(ease easeRate, zoom float64, loss chan<- struct{}) *lossRate {
	return &lossRate{
		Ease: ease,
		Zoom: zoom,
		Loss: loss,
		fall: float64(capRange),
	}
}

//
// 是否为丢包模式。
//
func (l *lossRate) Lost() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lost > 0
}

//
// 丢包评估。
// 确认距离越大，丢包概率越大。
// 速率越低，容忍的确认距离越小，反之越大。
// d 为确认距离。
//
// 每收到一个确认调用一次。
// 返回true表示一次丢包发生。
//
func (l *lossRate) Eval(d, base float64) bool {
	// 速率关系
	v := base / float64(BaseRate)
	// 距离关系
	if d*v < lossLimit {
		return false
	}
	l.Loss <- struct{}{}

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
func (l *lossRate) Rate(d, base float64) time.Duration {
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
// 慢减：先缓慢减速，越到后面减速越快（指数退避）。
//
// x 为发送距离，可能超过底部总值。
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
