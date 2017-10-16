//
// Package peeval 网络端点价值评估。
// 考虑三个方面：
//  1. 响应时间（RTT）；
//  2. 传输数据量和连接次数（活跃度）；
//  3. 破坏性行为评估；
//
package peeval

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// 基本常量。
const (
	UpLimit    = 0xff             // 满分上限值
	MaxAmount  = 1 << 34          // 满分数据量（16G）
	RTTTimeout = 20 * time.Second // RTT 超时线
)

//
// Peer 端点基本信息。
// 端点按IP识别，一段时间后取消IP标识的有效性。
// 注记：
// 不另设ID字段。会有隐私问题和长时间保持顾虑。
//
type Peer struct {
	IP   net.IP // 监听IP
	Port int    // 监听端口
}

//
// String 端点显示形式如 190.168.10.200:3456。
//
func (p Peer) String() string {
	return fmt.Sprintf("%s:%d", p.IP, p.Port)
}

//
// Resume 端点概要。
// 记录端点的行为评估因子。并发安全。
//  - 效益得分（Score）；
//  - 破坏性点数（Blame）；
//  - 连接效率（往返时间）；
// 注：
// RTT与端点查询筛选策略相关：假定RTT相似者相近。
//
type Resume struct {
	Peer
	Score               // 效益得分
	Blame               // 破坏性权衡
	RTT   time.Duration // Round Trip Time
}

//
// String 包含综合评分值。
// 取地址方式，因为Score/Blame中包含锁字段。
//
func (r *Resume) String() string {
	return fmt.Sprintf("%s[%d]", r.Peer, r.Value())
}

//
// Value 返回总评分值 [0-255]。
// Blame值两倍加权。
//
func (r *Resume) Value() int {
	v := r.Trips() + r.Hits() - r.Bads()*2

	if v < 0 {
		return 0
	}
	if v > UpLimit {
		return UpLimit
	}
	return v
}

//
// Trips 返回时间效率 [0-255]。
// 按20秒超时计算，约80ms可获得满分。
//
func (r *Resume) Trips() int {
	if r.RTT > RTTTimeout {
		return 0
	}
	v := int(RTTTimeout / r.RTT)

	if v < UpLimit {
		return v
	}
	return UpLimit
}

//
// Hits 返回效益分值 [0-255]。
//
func (r *Resume) Hits() int {
	return r.Score.Value()
}

//
// Bads 返回破坏值 [0-255]。
//
func (r *Resume) Bads() int {
	return r.Blame.Value()
}

//
// Score 得分评估器。
//  1. 端点活跃度；
//  2. 端点提供的有效数据量；
//
// 可能在多个协程中更新，故设计为并发安全。
//
type Score struct {
	counts int   // 连接次数（活跃度）
	amount int64 // 有效数据量（字节数）
	mu     sync.Mutex
}

//
// LinkIn 连接计数。
// 超过255次连接为满分。
//
func (s *Score) LinkIn() {
	s.mu.Lock()

	if s.counts < UpLimit {
		s.counts++
	}
	s.mu.Unlock()
}

//
// LinkOut 连接退出。
// 一般用于没有提供有效数据断开，或很快退出。
//
func (s *Score) LinkOut() {
	s.mu.Lock()

	if s.counts > 0 {
		s.counts--
	}
	s.mu.Unlock()
}

//
// Amount 数据量累计。
// 超过16G数据为满分。
// 注：应该只针对有效数据。
//
func (s *Score) Amount(sz int64) {
	s.mu.Lock()

	if s.amount < MaxAmount {
		s.amount += sz
	}
	if s.amount > MaxAmount {
		s.amount = MaxAmount
	}
	s.mu.Unlock()
}

//
// Value 计算并返回评估分值 [0-255]。
// 算法：
//  - 活跃度权重1/4；
//  - 有效数据权重3/4；
//
func (s *Score) Value() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	d := s.amount * UpLimit / MaxAmount
	return int(d*3/4) + s.counts/4
}

//
// Reset 状态重置。
// 通常在一定时间后调用，消除IP标识端点的失误。
//
func (s *Score) Reset() {
	s.mu.Lock()
	s.amount = 0
	s.counts = 0
	s.mu.Unlock()
}

//
// Blame 追责计算器。
//  1. 传递无效数据（交易/区块头等）；
//  2. 滥用连接资源（主观判断），危害中等；
//  3. 视为攻击性的行为；
//
// 需要用于不定数量的协程中，故设计并发安全。
//
type Blame struct {
	result int
	mu     sync.Mutex
}

//
// Invalid 传递无效数据。
// 包括无效的交易、区块头等。
// 普通缺点：计数加1个点。
//
func (t *Blame) Invalid() {
	t.mu.Lock()
	t.setVal(1)
	t.mu.Unlock()
}

//
// Abuse 滥用连接资源。
// 中等危害：计数加2个点。
//
func (t *Blame) Abuse() {
	t.mu.Lock()
	t.setVal(2)
	t.mu.Unlock()
}

//
// Attack 攻击性行为。
// 有意破坏：计数加3个点。
//
func (t *Blame) Attack() {
	t.mu.Lock()
	t.setVal(3)
	t.mu.Unlock()
}

//
// Value 返回攻击值 [0-255]。
//
func (t *Blame) Value() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.result
}

//
// Reset 重置计数。
//
func (t *Blame) Reset() {
	t.mu.Lock()
	t.result = 0
	t.mu.Unlock()
}

func (t *Blame) setVal(n int) {
	t.result += n

	if t.result > UpLimit {
		t.result = UpLimit
	}
}
