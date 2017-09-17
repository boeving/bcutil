//
// Package peeval 网络节点评估。
// （目标为众多节点，故包名后附s便于区别）
//
package peeval

import (
	"fmt"
	"net"
	"time"
)

// 基本常量。
const (
	RecordSize = 1000 // 每网络节点记录条目数。
	HitsMax    = 255  // 效益满分
	BadsMax    = 255  // 破坏性满分
)

// Peer 节点基本信息。
type Peer struct {
	IP   net.IP // 监听IP
	Port uint16 // 监听端口
}

//
// String 节点显示形式如 190.160.10.200:3356。
//
func (p *Peer) String() string {
	return fmt.Sprintf("%s:%d", p.IP, p.Port)
}

//
// Resume 节点概要。
// 记录节点的行为评估因子。
//  - 效益得分（Value）；
//  - 破坏性点数（Tough）；
//  - 连接效率（往返时间）；
// 注：
// RTT与节点查询筛选策略相关：假定RTT相似者相近。
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

// Value 得分评估器。
//  1. 节点活跃度；
//  2. 节点提供数据有效性；
//  3. 节点提供数据量；
type Value struct {
	counts int   // 连接次数（活跃度）
	amount int64 // 有效数据量（字节数）
	total  int64 // 总数据量（字节数）
}

//
// Tough 破坏计算器。
//  1. 无效交易；
//  3. 无效交易区块头；
//  2. 攻击性交易数据；
//
type Tough struct {
	badTx   int
	badHead int
	attack  int
}
