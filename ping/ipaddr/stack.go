package ipaddr

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/qchen-zh/pputil/tick"
)

//
// Stack IP序列。
// 兼容IPv4和IPv6两种格式。
// 内部用切片实现，可添加重复的地址。
//
// 并发安全：支持取地址服务期间添加/移除地址。
//
type Stack struct {
	pool []net.Addr
	mu   sync.Mutex
}

//
// NewStack 新建一个序列集。
//
func NewStack() *Stack {
	return &Stack{
		pool: []net.Addr{},
	}
}

//
// NewStackN 新建一个序列集。
// 如果已知集合大小，可传递一个初始值（优化）。
//
func NewStackN(sz int) *Stack {
	return &Stack{
		pool: make([]net.Addr, sz),
	}
}

//
// Add 添加一个IP地址。
// 如果地址格式错误，返回error。
// addr不含ipv6的Zone部分。
//
func (s *Stack) Add(addr string) error {
	ip := net.ParseIP(addr)
	if ip == nil {
		return fmt.Errorf("%s is not a valid IP address", addr)
	}
	s.mu.Lock()
	s.pool = append(s.pool, &net.IPAddr{IP: ip})
	s.mu.Unlock()

	return nil
}

//
// AddIP 添加一个IP地址。
// 可包含ipv6中的Zone字段。
//
func (s *Stack) AddIP(ip net.Addr) {
	s.mu.Lock()
	s.pool = append(s.pool, ip)
	s.mu.Unlock()
}

//
// AddsIP 添加多个IP地址。
// 可包含ipv6中的Zone字段。
//
func (s *Stack) AddsIP(ips []net.Addr) {
	s.mu.Lock()
	s.pool = append(s.pool, ips...)
	s.mu.Unlock()
}

//
// Pop 移除最后添加的几个地址。
// 返回移除的IP地址集，
// 若集合已空或删除量超出集合大小返回nil。
//
func (s *Stack) Pop(n int) []net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()

	sz := len(s.pool) - n

	if len(s.pool) == 0 || n == 0 || sz < 0 {
		return nil
	}
	as := s.pool[sz:]
	s.pool = s.pool[:sz]

	return as
}

//
// IPAddrs 获取IP序列的微服务。
// 实现 ping.Address 接口。
//
func (s *Stack) IPAddrs(t time.Duration, cancel func() bool) <-chan net.Addr {
	ch := make(chan net.Addr)

	go func() {
		tk := tick.NewTicker(t)
		for i := 0; i < len(s.pool); i++ {
			if cancel != nil && cancel() {
				break
			}
			ch <- s.Get(i)
			tk.Tick()
		}
		close(ch)
		tk.Stop()
	}()

	return ch
}

//
// Get 提取下一个地址。
// 允许地址集在服务期变化（更大的灵活性）。
// 用单独的函数调用，使得锁可以即时释放。
//
func (s *Stack) Get(i int) net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pool[i]
}
