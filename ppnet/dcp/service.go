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
	baseLine int
}

//
// 发送方服务。
//
type sendService struct {
}

//
// 接收端服务。
//
type recvService struct {
}
