//
// Package peeval 网络端点价值评估。
//
package peeval

import (
	"fmt"
	"net"
	"time"
)

// 基本常量。
const (
	HitsMax = 0xff // 效益满分
	BadsMax = 0xff // 破坏性满分
)

// Peer 端点基本信息。
type Peer struct {
	IP   net.IP // 监听IP
	Port uint16 // 监听端口
}

//
// String 端点显示形式如 190.160.10.200:3356。
//
func (p *Peer) String() string {
	return fmt.Sprintf("%s:%d", p.IP, p.Port)
}

//
// Resume 端点概要。
// 记录端点的行为评估因子。
//  - 效益得分（Score）；
//  - 破坏性点数（Tough）；
//  - 连接效率（往返时间）；
// 注：
// RTT与端点查询筛选策略相关：假定RTT相似者相近。
//
type Resume struct {
	Peer
	Logon, Last time.Time     // 登记/最近一次连接时间
	RTT         time.Duration // Round Trip Time
	hits        uint8         // 效益得分
	bads        uint8         // 破坏性
}

// Hits 返回得分值。
func (rs *Resume) Hits() uint8 {
	return rs.hits
}

//
// HitsUp 增加效益得分。
// 满分之后不再增加。
// 增加之后超出满分则设置为满分。
//
func (rs *Resume) HitsUp(n uint8) uint8 {
	if n+rs.hits < HitsMax {
		rs.hits += n
	} else {
		rs.hits = HitsMax
	}
	return rs.hits
}

//
// HitsDown 减少效益得分。
// 零分之后不再减少。
//
func (rs *Resume) HitsDown(n uint8) uint8 {
	if rs.hits-n > 0 {
		rs.hits -= n
	} else {
		rs.hits = 0
	}
	return rs.hits
}

// Bads 返回破坏值。
func (rs *Resume) Bads() uint8 {
	return rs.bads
}

//
// BadAgain 破坏性增加一个点数。
// 达到最大值后不再增加。
//
func (rs *Resume) BadAgain(n uint8) uint8 {
	if n+rs.bads < BadsMax {
		rs.bads += n
	} else {
		rs.bads = BadsMax
	}
	return rs.bads
}

// Score 得分评估器。
//  1. 端点活跃度；
//  2. 端点提供数据有效性；
//  3. 端点提供数据量；
type Score struct {
	counts int   // 连接次数（活跃度）
	amount int64 // 有效数据量（字节数）
	total  int64 // 总数据量（字节数）
}

// Count 连接计数加一。
func (s *Score) Count() {
	if s.counts < 0 {
		return
	}
	s.counts++
}

// Amount 有效数据量累计。
func (s *Score) Amount(sz int64) {
	if s.amount < 0 {
		return
	}
	s.amount += sz
}

// Total 总数据量累计。
func (s *Score) Total(sz int64) {
	if s.total < 0 {
		return
	}
	s.total += sz
}

//
// Value 返回评估分值。
// 当各字段值为负时表明已经最大化了。
//
func (s *Score) Value() uint8 {
	//
}

//
// Tough 破坏计算器。
//  1. 传递无效交易计数；
//  3. 传递无效交易区块头计数；
//  2. 传递攻击性交易数据计数；
//
type Tough struct {
	badTx   int
	badHead int
	attack  int
}

// BadTx 无效交易次数累计。
func (t *Tough) BadTx() {
	if t.badTx < 0 {
		return
	}
	t.badTx++
}

// BadHead 无效区块头计次。
func (t *Tough) BadHead() {
	if t.badHead < 0 {
		return
	}
	t.badHead++
}

// Attack 攻击数据计次。
func (t *Tough) Attack() {
	if t.attack < 0 {
		return
	}
	t.attack++
}

//
// Value 返回攻击值。
// 当各字段值为负时表明已经最大化了。
//
func (t *Tough) Value() uint8 {
	//
}
