//
// Package peers 网络节点评估。
// （目标为众多节点，故包名后附s便于区别）
//
package peers

import (
	"fmt"
	"net"
	"time"
)

// RecordSize 每网络节点记录条目数。
const RecordSize = 1000

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
// 效益因素：
//  1. 节点活跃度（连接次数）；
//  2. 节点提供数据有效性；
//  3. 节点提供数据量；
//
// 破坏性因素：
//  1. 传递无效交易；
//  2. 传递攻击性交易数据；
//  3. 传递无效交易区块头；
//
// 往返时间（RTT）：
// 与节点查询筛选策略相关：假定RTT相似者相近。
//
type Resume struct {
	Peer
	Logon, Last time.Duration // 登记/最近一次连接时间
	Hits        uint8         // 效益得分
	Bads        uint8         // 破坏性
	RTT         time.Duration // Round Trip Time
}
