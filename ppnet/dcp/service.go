package dcp

////////////////////
// DCP 内部服务实现。
//
///////////////////////////////////////////////////////////////////////////////

import (
	"time"
)

// 基础常量设置。
const (
	baseRate    = 5 * time.Millisecond   // 基础速率。初始默认发包间隔。
	minRate     = 100 * time.Microsecond // 极限速率。万次/秒
	rateUpdate  = 500 * time.Millisecond // 速率更新间隔
	aliveProbes = 6                      // 保活探测次数上限
	aliveTime   = 120 * time.Second      // 保活时间界限，考虑NAT会话存活时间
	aliveIntvl  = 10 * time.Second       // 保活报文间隔时间
)

//
// service 底层DCP服务。
// 一个实例对应一个4元组两端连系。
//
type service struct {
	LineCaps int // 线路容量（数据报个数）
}

//
// 发送方服务。
// 实现发送方的控制逻辑：速率、速率调整，丢包判断。
//
// ACK滞速：
//
type servSend struct {
	RE       rateEval      // 速率评估器
	Rate     time.Duration // 当前发送速率（μs）
	Progress uint16        // 进度（确认号）
}

//
// 接收端服务。
//
type servRecv struct {
	Progress uint16 // 进度（实际）
	ProgLine uint16 // 控制进度线
}

//
// 速率评估。
// 注：不含初始段的匀速（无评估参考）。
// 距离：
//  - 发送距离越大，是因为对方确认慢，故减速。
//  - 确认距离越大，丢包概率越大，故减速。设置丢包认可。
// 数据量：
//  - 数据量越小，基准速率向慢（稳）。降低丢包评估重要性。
//  - 数据量越大，基准速率趋快（急）。丢包评估越准确。
// 因子变量：
//  1. 基线。即基准速率的自动调优值。
//  2. 斜率。速率增幅，越接近基线则越慢。
//  3. 跌幅。丢包之后的减速量（快减），拥塞响应。
//
type rateEval struct {
	BaseRate time.Duration // 基准速率
	Slope    int           // 斜率。速率增幅[0-100]
	Decline  int           // 跌幅。丢包减速量
	IsLost   bool          // 丢包认可
}
