package stunx

//
// NATMode NAT 穿透模式。
//
type NATMode uint32

// 4种穿透类型
const (
	FullCone  NATMode = 1 << iota // 1: Full Cone NAT
	RestCone                      // 2: Restricted Cone NAT
	PortRCone                     // 4: Port Restricted Cone NAT
	Symmetric                     // 8: Symmetric NAT
)

//
// StunFeasible NAT 可穿透性。
//
var StunFeasible = map[NATMode]NATMode{
	// 接受任意连接。
	FullCone: FullCone | RestCone | PortRCone | Symmetric,

	// 发送IP打洞后，接受任意连接。
	RestCone: FullCone | RestCone | PortRCone | Symmetric,

	// 发送IP和端口打洞后，接收除Symmetric外的任意连接。
	// 因为无法确定Symmetric的端口。
	PortRCone: FullCone | RestCone | PortRCone,

	// 不接受连接（仅连出）。
	Symmetric: 0,
}
