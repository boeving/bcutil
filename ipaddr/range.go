//
// Package ipaddr IP地址集封装。
// 包含一个范围类型和一个序列集合类型。
//
package ipaddr

import (
	"fmt"
	"net"
	"sync"
)

//
// Range IP范围。
// 仅支持IPv4地址类型。
//
// 并发安全，可以在多个Go程中操作同一实例。
//
type Range struct {
	netip  net.IP // 网络地址
	begin  int    // 起始主机号
	end    int    // 终点主机号
	cursor int    // 当前取值
	mu     sync.Mutex
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
	return &Range{netip: ipn.IP, begin: begin, end: end}, nil
}

//
// IPAddrs 获取IP序列的管道。
//
func (r *Range) IPAddrs(cancel func() bool) <-chan net.Addr {
	ch := make(chan net.Addr)

	go func() {
		for {
			addr := r.Next()
			if cancel() || addr == nil {
				break
			}
			ch <- addr
		}
		close(ch)
	}()

	return ch
}

//
// Reset 迭代重置。内部游标归零。
//
func (r *Range) Reset() *Range {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cursor = 0
	return r
}

//
// Next 获取下一个IP地址。
// 到终点后返回nil。
//
func (r *Range) Next() net.Addr {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cursor >= r.end {
		return nil
	}
	r.cursor++
	return makeIPv4(r.netip, iToBytes(uint32(r.cursor-1)))
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
		return fmt.Errorf("%s is not IPv4 address", cidr)
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
//
func makeIPv4(nip net.IP, host []byte) *net.IPAddr {
	out := make([]byte, len(nip))

	for i := 0; i < net.IPv4len; i++ {
		out[i] = nip[i] | host[i]
	}
	return &net.IPAddr{IP: net.IP(out)}
}

//
// 检查子网段主机号是否在有效范围内。
//
func validHost(mask net.IPMask, host int) bool {
	ones, bits := mask.Size()

	if 1<<uint(bits-ones) <= host {
		return false
	}
	return true
}

//
// 整数转为字节序列。
//
func iToBytes(n uint32) []byte {
	out := make([]byte, 4)

	for i := 3; i >= 0; i-- {
		out[i] = byte(n & 0xff)
		n >>= 8
	}
	return out
}
