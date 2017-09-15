//
// Package peers 网络节点评估。
//
package peers

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// 基本常量设定。
const (
	// 统一为IPv6长度
	LenIP   = net.IPv6len
	LenPort = 2
	LenBoth = LenIP + LenPort

	// 每网络节点记录条目数
	RecordSize = 1000
)

// Peer 已知的确定节点。
type Peer struct {
	IP   net.IP `json:"ip"`             // 监听IP
	Port uint16 `json:"port,omitempty"` // 监听端口
}

//
// String 节点显示形式如 190.160.10.200:3356。
//
func (p *Peer) String() string {
	return fmt.Sprintf("%s:%d", p.IP, p.Port)
}

//
// SetBytes 用字节序列设置对象。
// 传递的bs参数固定长度为10。采用小端字节序。
//
func (p *Peer) SetBytes(bs []byte) bool {
	if len(bs) != LenBoth {
		return false
	}
	p.IP = net.IP(bs[:LenIP])
	p.Port = binary.LittleEndian.Uint16(bs[LenIP:])

	return true
}

//
// Bytes 返回字节序列表示（IP[16]Port[2]）。
// 采用小端字节序。
//
func (p *Peer) Bytes() []byte {
	buf := make([]byte, LenPort)
	binary.LittleEndian.PutUint16(buf, p.Port)

	return append([]byte(p.IP.To16()), buf...)
}

//
// Score 节点评估。
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
type Score struct {
	Peer
	Logon, Last time.Duration // 登记/最近一次连接时间
	Hits        int           // 效益得分
	Bads        int           // 破坏性
	RTT         time.Duration // Round Trip Time
}

//
// NetName 节点网络名。
// config.json 中 stakes 的配置键名。
// 注：findings 为系统自身名称，拥有独占性。
//
type NetName string

//
// Record 节点评估记录。
// 以节点IP的字符串值为键（IP.String()）。
//
// 系统会定时对缓存进行清理，
// 清理为惰性式：清理到大小满足要求即终止。
//
type Record map[string]Score

// Records 各类网络节点评估记录集。
type Records map[NetName]Record
