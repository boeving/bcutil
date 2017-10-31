package download

/////////////////////////////////////////
// 缓存已下载数据并为其它端点提供下载支持。
// - 从其它端点接收的分片数据；
// - 载入本地存储的有效下载分片；
///////////////////////////////////////////////////////////////////////////////

import (
	"errors"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/qchen-zh/pputil/download/piece"
)

// CacheSize 默认缓存大小（100MB）
const CacheSize = 1 << 20 * 100

// 到达上限后，缩减量
// 概略值。
const pruneSize = 50

// 缓存的分片数据。
// （附带访问时间戳）
type cacheData struct {
	bs    []byte
	visit time.Time
}

// 缓存索引队列。
// 缓存超限清理时使用。
type indexQueue struct {
	off   int64
	visit time.Time
}

//
// Cache 单根缓存。
// 针对单个目标文件（或完整数据）。对应一个根哈希。
//
type Cache struct {
	from  io.ReaderAt          // 外部存储源
	span  int                  // 分片大小（bytes）
	list  map[int64]*cacheData // 数据缓存
	rest  map[int64]struct{}   // 剩余分片集（未下载）
	limit int                  // 缓存条目数上限
	mu    sync.Mutex
}

//
// NewCache 新建一个缓存器。
// span 为目标文件的分片大小（字节数），用于约束从文件中取数据。
// cmax 为缓存大小限制。
// 已经完成的文件分享同样受此约束，以阻止大片数据的攻击性请求。
//
func NewCache(rd io.ReaderAt, span int, rest map[int64]struct{}, cmax int) *Cache {
	return &Cache{
		from:  rd,
		span:  span,
		rest:  rest,
		list:  make(map[int64]*cacheData),
		limit: cmax / span,
	}
}

//
// Get 获取特定起点下标的分片数据。
// 缓存中没有时，如果已下载则从文件中导入并缓存。否则返回nil。
//
func (c *Cache) Get(k int64) *PieceData {
	c.mu.Lock()
	defer c.mu.Unlock()

	if d, ok := c.list[k]; ok {
		d.visit = time.Now()
		return &PieceData{k, d.bs}
	}
	if _, ok := c.rest[k]; ok {
		return nil
	}
	d, err := c.load(k)
	if err != nil {
		return nil
	}
	if len(c.list) >= c.limit {
		c.checkLimit(pruneSize)
	}
	// store
	c.list[k] = &cacheData{d, time.Now()}

	return &PieceData{k, d}
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
		c.checkLimit(pruneSize)
	}
	c.list[k] = &cacheData{d, time.Now()}

	return true
}

//
// 从外部存储中载入目标位置的分片。
// 目标分片数据应该已经存在。
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
// sz 为调整大小（条目数）。概略值。
//
func (c *Cache) checkLimit(sz int) {
	buf := make([]*indexQueue, 0, len(c.list))

	for off, cd := range c.list {
		buf = append(buf, &indexQueue{off, cd.visit})
	}
	sort.Slice(
		buf,
		func(i, j int) bool { return buf[i].visit.Before(buf[j].visit) },
	)
	// 大分片拥有小数量缓存
	// 此时按1/3比例缩减。
	if sz > len(buf)/3 {
		sz = len(buf) / 3
	}
	for i := 0; i < sz; i++ {
		delete(c.list, buf[i].off)
	}
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
