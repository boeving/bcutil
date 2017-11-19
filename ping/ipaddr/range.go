// Package ipaddr IP地址集封装。
// 包含一个范围类型和一个序列集合类型。
//
package ipaddr

/////////////////
// 特殊用途地址
// 0.0.0.0/8 		（本网络/源地址）
// ---------------------------------
// 10.0.0.0/8 		（专/私用）
// 127.0.0.0/8 		（环回）
// 172.16.0.0/12 	（专/私用）
// 192.0.0.0/24 	（IANA保留）
// 192.0.2.0/24 	（TEST-NET-1，文档和示例）
// 192.88.99.0/24 	（6to4中继）
// 192.168.0.0/16 	（专/私用）
// 198.18.0.0/15 	（网络基准测试）
// 198.51.100.0/24	（TEST-NET-2，文档和示例）
// 203.0.113.0/24 	（TEST-NET-3，文档和示例）
// ------------------------------------------
// 224.0.0.0/4		（多播，D类网络）
// 240.0.0.0/4		（保留，E类网络）
// 255.255.255.255	（广播）
///////////////////////////////////////////////////////////////////////////////

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/qchen-zh/pputil/tick"
)

//
// Range IP范围。
// 仅支持IPv4地址类型。
//
type Range struct {
	netip net.IP // 网络地址
	begin int    // 起始主机号
	end   int    // 主机号终止边界（不含）
}

// NewRange 新建一个范围实例。
// cidr 为网络IP与子网掩码格式的字符串，如："192.0.2.0/24"
// 仅支持IPv4。
// first为起始主机号，last为终点主机号。
func NewRange(cidr string, begin, end int) (*Range, error) {
	ipn, err := parse(cidr)
	if err != nil {
		return nil, err
	}
	err = check(begin, end, ipn.Mask)
	if err != nil {
		return nil, err
	}
	return &Range{ipn.IP, begin, end}, nil
}

//
// IPAddrs 获取IP序列的微服务。
// 实现 ping.Address 接口。
//
func (r *Range) IPAddrs(t time.Duration, cancel func() bool) <-chan net.Addr {
	ch := make(chan net.Addr)

	go func() {
		tk := tick.NewTicker(t)
		for i := r.begin; i < r.end; i++ {
			if cancel != nil && cancel() {
				break
			}
			ch <- r.Get(i)
			tk.Tick()
		}
		close(ch)
		tk.Stop()
	}()

	return ch
}

//
// Get 获取一个IP地址。
//
func (r *Range) Get(k int) net.Addr {
	var host [4]byte
	binary.BigEndian.PutUint32(host[:], uint32(k))

	return makeIPv4(r.netip, host)
}

/////////////
// 私有辅助
///////////////////////////////////////////////////////////////////////////////

//
// 网络地址解析。
//
func parse(cidr string) (*net.IPNet, error) {
	ip, ipn, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	if len(ip) != net.IPv4len {
		return nil, fmt.Errorf("%s is not IPv4 address", cidr)
	}
	return ipn, nil
}

//
// 范围合法性检查。
//
func check(begin, end int, mask net.IPMask) error {
	if begin >= end || !validHost(mask, end) {
		return fmt.Errorf("[%d, %d] host ip range is valid", begin, end)
	}
	return nil
}

//
// 构造IPv4主机地址。
// nip 为网络IP地址，host 为主机号字节序列。
//
func makeIPv4(nip net.IP, host [4]byte) net.Addr {
	var out [4]byte

	for i := 0; i < 4; i++ {
		out[i] = nip[i] | host[i]
	}
	return &net.IPAddr{IP: net.IP(out[:])}
}

//
// 检查子网段主机号是否在有效范围内。
//
func validHost(mask net.IPMask, host int) bool {
	ones, bits := mask.Size()
	return 1<<uint(bits-ones) <= host
}
