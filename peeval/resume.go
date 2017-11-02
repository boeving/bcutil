// Package peeval 网络端点价值评估。
// 仅用IP和端口对目标端点进行短时间的标识和评估，不设计特别的ID标识（会增加复杂性并带来隐私问题）。
// 端点价值可能用于连接&数据传递优先性，或暂时性屏蔽。
//
// 评估的四个方面：
//  1. 平均响应时间（RTT）；
//  2. 传输的有效数据量；
//  3. 破坏性行为减分；
//  4. 通用的3级加分（如增值服务）；
//
package peeval

import (
	"fmt"
	"net"
	"time"
)

// Score 记分类型。
type Score int

// 行为与分值
const (
	Good    Score = 1  // 普通加分
	Better  Score = 2  // 较好加分
	Best    Score = 4  // 最高加分
	BadData Score = -1 // 传递无效数据
	Abuse   Score = -2 // 滥用资源（对端）
	Attack  Score = -4 // 恶意攻击
)

// 基本常量。
const (
	UpLimit    = 0xff             // 满分上限值
	MaxAmount  = 1 << 33          // 满分数据量（8G）
	RTTTimeout = 20 * time.Second // RTT 超时线
)

//
// PeerID 端点标识。
//
type PeerID struct {
	IP   net.IP
	Port int
}

//
// String 类似URL形式。
// 如 190.168.10.2:6500。
//
func (p PeerID) String() string {
	return fmt.Sprintf("%s:%d", p.IP, p.Port)
}

//
// Resume 端点概要。
//
type Resume struct {
	ID    PeerID        // 端点标识
	rtt   time.Duration // 连通效率（平均）
	score Score         // 积分值
	dsize int64         // 有效数据量
}

//
// String 包含综合评分值。
// 格式：ID[分值]
//
func (r Resume) String() string {
	return fmt.Sprintf("%s[%d]", r.ID, r.Value())
}

//
// Plus 累计记分。
// 实参通常为预定义的几个常量值。
//
// 参数未强制约束范围，因此外部可传递一个差距很大的值。
// 明确的类型转换表示用户意愿。
//
func (r *Resume) Plus(n Score) {
	r.score += n
}

//
// TimeTrip 平均时间（RTT）效率。
//
func (r *Resume) TimeTrip(tm time.Duration) {
	r.rtt = (r.rtt + tm) / 2
}

//
// Supply 有效数据累计。
//
func (r *Resume) Supply(sz int64) {
	r.dsize += sz
}

//
// Value 端点评估值 [0-255]。
// 积分值没有255的上限，故可能抢占优势。
//
func (r *Resume) Value() int {
	v := (r.trips() + int(r.score) + r.dvalue()) / 3

	if v < 0 {
		return 0
	}
	if v > UpLimit {
		return UpLimit
	}
	return v
}

//
// 时间效率得分 [0-255]。
// 按20秒超时计算，约80ms可获得满分。
//
func (r *Resume) trips() int {
	if r.rtt > RTTTimeout {
		return 0
	}
	v := int(RTTTimeout / r.rtt)

	if v < UpLimit {
		return v
	}
	return UpLimit
}

//
// 数据量评估得分 [0-255]。
//
func (r *Resume) dvalue() int {
	if r.dsize >= MaxAmount {
		return UpLimit
	}
	return int(r.dsize * int64(UpLimit) / int64(MaxAmount))
}
