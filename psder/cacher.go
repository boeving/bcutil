package psder

/////////////////////////////////////////
// 缓存已下载数据并为其它端点提供下载支持。
// - 从其它端点接收的分片数据；
// - 载入本地存储的有效下载分片；
///////////////////////////////////////////////////////////////////////////////

import (
	"errors"
	"io"
	"log"
	"sync"
	"time"

	"github.com/qchen-zh/bcutil/psder/piece"
)

// CacheSize 默认缓存大小（100MB）
const CacheSize = 1 << 20 * 100

// 缓存的分片数据。
// （附带访问时间戳）
type cacheData struct {
	bs    []byte
	visit time.Time
}

//
// Cache 单根缓存。
// 针对单个目标文件（或完整数据）。对应一个根哈希。
// 方法为并发安全。
//
type Cache struct {
	from   io.ReaderAt          // 外部存储源
	span   int                  // 分片大小（bytes）
	list   map[int64]*cacheData // 数据缓存
	rest   map[int64]struct{}   // 剩余分片集（未下载）
	limit  int                  // 缓存条目数上限
	oldest time.Time            // 缓存集最老条目时间
	mu     sync.Mutex
}

//
// NewCache 新建一个缓存器。
// span 为目标文件的分片大小（字节数），用于约束从文件中取数据。
// cmax 为缓存大小上限值（CacheSize）。
// 已经完成的文件分享同样受此约束，以阻止大片数据的攻击性请求。
//
func NewCache(rd io.ReaderAt, span int, rest map[int64]struct{}, cmax int) *Cache {
	return &Cache{
		from:   rd,
		span:   span,
		rest:   rest,
		list:   make(map[int64]*cacheData),
		limit:  cmax / span,
		oldest: time.Now(),
	}
}

//
// Get 获取特定起点下标的分片数据。
// 缓存中没有时，如果已下载则从文件中导入并缓存。否则返回nil。
//
func (c *Cache) Get(k int64) *PieceData {
	if pd, ok := c.cache(k); ok {
		return pd
	}
	d, err := c.load(k)
	if err != nil {
		log.Printf("load %d offset piece: %s", k, err)
		return nil
	}
	c.mu.Lock()
	// 已导入新条目，可触发清理。
	if len(c.list) >= c.limit {
		c.checkLimit()
	}
	// 缓存入
	c.list[k] = &cacheData{d, time.Now()}
	c.mu.Unlock()

	return &PieceData{k, d}
}

// 从缓存中取值。
// 无法从缓存中取值时，bool值为false。
func (c *Cache) cache(k int64) (*PieceData, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if d, ok := c.list[k]; ok {
		d.visit = time.Now()
		return &PieceData{k, d.bs}, true
	}
	if _, ok := c.rest[k]; ok {
		return nil, true
	}
	return nil, false
}

//
// Add 添加分片缓存。
// 如果目标分片已经存在即不再存储，返回false，否则返回true。
//
func (c *Cache) Add(k int64, d []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.list[k]; ok {
		return false
	}
	if len(c.list) >= c.limit {
		c.checkLimit()
	}
	c.list[k] = &cacheData{d, time.Now()}

	return true
}

//
// RestOut 剩余分片标记索引移除。
// 即外部对off下标的分片已经完成下载，可以提供分享了。
//
func (c *Cache) RestOut(off int64) {
	c.mu.Lock()
	delete(c.rest, off)
	c.mu.Unlock()
}

//
// 从外部存储中载入目标位置的分片。
// 通常，目标分片数据应该已经存在。
// （ReadAt 并发安全）
//
func (c *Cache) load(off int64) ([]byte, error) {
	buf := make([]byte, c.span)

	n, err := c.from.ReadAt(buf, off)
	if err != nil {
		return nil, err
	}
	if n != int(c.span) {
		return nil, errors.New("invalid piece data")
	}
	return buf, nil
}

//
// 检查缓存上限并清理不常用的条目。
// 清理策略：
// 记忆最早时间，清理到当前时间1/5（20%）时段的部分。
// 注：避免排序
//
func (c *Cache) checkLimit() {
	cur := time.Now()
	all := cur.Sub(c.oldest)
	low := c.oldest.Add(all / 5)

	for off, cd := range c.list {
		// 清理下限之前的条目
		if cd.visit.Before(low) {
			delete(c.list, off)
			continue
		}
		// 剩余缓存中最老条目
		if cd.visit.Before(cur) {
			cur = cd.visit
		}
	}
	c.oldest = cur
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
