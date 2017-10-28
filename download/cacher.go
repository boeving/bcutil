package download

/////////////////////////////////////////
// 缓存已下载数据并为其它端点提供下载支持。
// - 从其它端点接收的分片数据；
// - 载入本地存储的有效下载分片；
///////////////////////////////////////////////////////////////////////////////

import (
	"io"
	"sync"

	"github.com/qchen-zh/pputil/download/piece"
)

//
// Cache 单根缓存。
// 针对单个目标文件（或完整数据）。对应一个根哈希。
//
type Cache struct {
	list map[int64]*PieceData
	mu   sync.Mutex
}

//
// NewCache 新建一个缓存器。
//
func NewCache() *Cache {
	return &Cache{
		list: make(map[int64]*PieceData),
	}
}

//
// Get 获取特定起点下标的分片数据。
// 无缓存值时返回 nil。
//
func (c *Cache) Get(k int64) *PieceData {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.list[k]
}

//
// Add 添加分片缓存。
// 如果目标分片已经存在即不再存储，返回false，否则返回true。
//
func (c *Cache) Add(k int64, pd *PieceData) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.list[k]; ok {
		return false
	}
	c.list[k] = pd
	return true
}

//
// Load 载入本地存储的数据到缓存。
// 通常用于分享文件时使用，在初始阶段执行。
//
// 索引文件index为已下载数据文件data的分片定义，
// 未下载索引rest用于排除index中无效的部分。
//
// 引入rest是为了充分分享已经下载的数据，
// 若等完全下载后才载入分享，可能分享的效果并不好（不充分和可能的自私）。
// 外部可恰当/随机的导入部分文件规划分享。
//
func (c *Cache) Load(data io.ReaderAt, index, rest io.Reader) error {
	return nil
}

//
// Pool 缓存池。
// 汇集当前下载的文件/数据的缓存。
//
type Pool struct {
	buf map[piece.HashSum]*Cache
	mu  sync.Mutex
}

//
// NewPool 新建一个缓存池。
//
func NewPool() *Pool {
	return &Pool{
		buf: make(map[piece.HashSum]*Cache),
	}
}

//
// Get 获取目标单个缓存。
// 返回值为nil表示没有目标值。
//
func (p *Pool) Get(k piece.HashSum) *Cache {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf[k]
}

//
// Store 存储目标单根缓存。
// 如果已经存在则不再加入，返回false，否则返回true。
//
func (p *Pool) Store(k piece.HashSum, c *Cache) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.buf[k]; ok {
		return false
	}
	p.buf[k] = c
	return true
}
