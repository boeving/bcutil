package download

import (
	"io"

	"github.com/qchen-zh/pputil/download/piece"
	"github.com/qchen-zh/pputil/goes"
)

type RestPieces = piece.RestPieces

// Status 下载状态。
type Status struct {
	Total     int // 总分片数
	Completed int // 已完成下载分片数
}

//
// Rest 剩余量设置。
//
func (s *Status) Rest(v int) {
	s.Completed = s.Total - v
}

//
// Progress 完成进度[0-1]。
//
func (s *Status) Progress() float32 {
	if s.Completed == s.Total {
		return 1.0
	}
	return float32(s.Completed) / float32(s.Total)
}

//
// Cacher 缓存器（并发实现）。
//
type Cacher struct {
	data []PieceData     // 分片数据集
	out  io.WriterAt     // 缓存输出
	ch   chan PieceData  // 服务通道
	done func(off int64) // 每存储成功回调
}

//
// NewCacher 新建一个缓存器。
//
func NewCacher(pd []PieceData, out io.WriterAt, fx func(int64)) *Cacher {
	cc := Cacher{
		pd,
		out,
		make(chan PieceData),
		fx,
	}
	go func() {
		for _, pd := range cc.data {
			cc.ch <- pd
		}
		close(cc.ch)
	}()
	return &cc
}

//
// Task 获取每一次分片任务。
//
func (c *Cacher) Task() (k interface{}, ok bool) {
	k, ok = <-c.ch
	return
}

//
// Work 完成每片数据存储。
//
func (c *Cacher) Work(k interface{}) error {
	v := k.(PieceData)

	if n, err := c.out.WriteAt(v.Bytes, v.Offset); err != nil {
		return err
	}
	c.done(v.Offset)

	return nil
}

//
// Server 下载服务器。
//  - 依缓存大小定义及时存储下载数据；
//  - 定时存储未下载分片索引信息；
//  - 监查User状态，决定自己的下载行为；
//  - 更新下载进度数据；
//
type Server struct {
	User    Monitor       // 下载监控器
	Dler    Downloader    // 下载器
	Speed   Status        // 完成进度
	Indexer io.ReadWriter // 索引缓存
	Outer   io.WriterAt   // 数据缓存输出
	OutSize int           // 输出最低值

	dtch <-chan PieceData // 数据传递通道
	rest RestPieces       // 剩余分片索引集
}

//
// Run 开启下载服务。
// 如果有下载失败的分片，重启一个子服务。
//
//  - 读取分片索引缓存，更新状态；
//  - 调用Monitor.Start()，返回true则开始下载；
//
func (s *Server) Run() {

}

//
// 启动一个下载服务。
// 针对目标分片定义执行下载任务。
//
// 返回的通道如果传递错误，说明有未下载成功的分片。
// 外部检测该信息，必要时重启一个服务即可。
//
func (s *Server) serve(rest RestPieces) <-chan error {
	s.dtch = s.Dler.Run(rest.Pieces)
	s.rest = RestPieces{rest.Clone()}

	ch := make(chan error)

	// 持续读取下载传递来的数据，
	// 按缓存配置（CacheSize：OutSize）逐批写入存储。
	// @n 每存储分片数。
	go func(n int) {
		buf := make([]PieceData, 0, n)
	L:
		for pd := range s.dtch {
			// 状态控制
			select {
			case <-s.User.ChExit():
				break L
			case <-s.User.ChPause():
				// blocking or through
			}
			buf = append(buf, pd)
			if len(buf) < n {
				continue
			}
			// 开启一个异步存储
			pbs := buf
			go func() {
				for err := range saveCache(pbs, s.Outer) {
					ch <- err
				}
			}()
			buf = make([]PieceData, 0, n)
		}
		if len(buf) > 0 {
			// 末尾批次
			for err := range saveCache(buf, s.Outer) {
				ch <- err
			}
		}
		close(ch) // end!
	}(s.OutSize / rest.Span)

	return ch
}

//
// 缓存存储到外部。
//
func (s *Server) saveCache(pd []PieceData) <-chan error {
	done := func(k int64) {
		// 更新剩余分片索引信息。
		delete(s.rest.Index[k])
	}
	cc := NewCacher(pd, s.Outer, done)

	// 取集合大小的一半为并发Go程量，
	// 仅是一个简单的直觉处理。
	limit := len(pd) / 2

	return goes.WorksLong(goes.LimitTasker(&cc, limit), nil)
}
