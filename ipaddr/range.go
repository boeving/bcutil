//
// Package ipaddr IP地址集封装。
// 包含一个范围类型和一个序列集合类型。
//
package ipaddr

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/qchen-zh/pputil/goes"
)

//
// Range IP范围。
// 仅支持IPv4地址类型。
//
type Range struct {
	netip      net.IP // 网络地址
	begin, end uint32 // 起止主机号（不含end）
}

// NewRange 新建一个范围实例。
// cidr 为网络IP与子网掩码格式的字符串，如："192.0.2.0/24"
// 仅支持IPv4。
// first为起始主机号，last为终点主机号。
func NewRange(cidr string, begin, end uint32) (*Range, error) {
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
// IPAddrs 获取IP序列的微服务。
// 返回管道内值：net.Addr。
//
func (r *Range) IPAddrs(cancel func() bool) <-chan interface{} {
	return goes.IntGets(r, int(r.begin), 1, cancel)
}

//
// IntGet 获取一个IP地址。
// v存储：net.Addr
//
func (r *Range) IntGet(k int) (interface{}, bool) {
	i := uint32(k)
	if i >= r.end {
		return nil, false
	}
	var host [4]byte
	binary.BigEndian.PutUint32(host[:], i)

	return makeIPv4(r.netip, host), true
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
func check(begin, end uint32, mask net.IPMask) error {
	if begin >= end || !validHost(mask, end) {
		return fmt.Errorf("[%d, %d] host ip range is valid", begin, end)
	}
	return nil
}

//
// 构造IPv4主机地址。
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
func validHost(mask net.IPMask, host uint32) bool {
	ones, bits := mask.Size()
	return 1<<uint(bits-ones) <= host
}
