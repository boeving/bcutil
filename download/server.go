package download

import (
	"io"
	"log"
	"sync"
	"time"

	"github.com/qchen-zh/pputil"
	"github.com/qchen-zh/pputil/download/piece"
	"github.com/qchen-zh/pputil/goes"
)

const (
	// IndexInterval 索引存储默认间隔时间。
	IndexInterval = 5 * time.Minute
)

type (
	HashSum    = piece.HashSum
	RestPieces = piece.RestPieces
)

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
//  - 依缓存大小及时存储下载的数据；
//  - 定时保存未下载分片索引信息（更新）；
//  - 监查User状态，决定自己的下载行为；
//  - 更新下载进度数据；
//
type Server struct {
	User     Monitor       // 下载监控器
	Dler     Downloader    // 下载器
	Indexer  io.WriterAt   // 索引缓存
	Outer    io.WriterAt   // 数据缓存输出
	OutSize  int           // 输出最低值
	Interval time.Duration // 索引保存间隔时间

	dtch  <-chan PieceData // 数据传递通道
	speed *Status          // 完成进度
	rtsem chan struct{}    // 分片索引管理锁
}

//
// Run 开启下载服务。
// 如果有下载失败的分片，自动开启下一轮服务。
// rest参数由外部提供，
// 可能是一个剩余分片集，也可能由一个初始分片集构造。
//
// rest 会被修改，外部不应再使用。
//
func (s *Server) Run(rest RestPieces) {
	if !s.User.Start() || rest.Empty() {
		return
	}
	s.speed = s.User.Status()
	s.dtch = s.Dler.Run(rest.Span, rest.Indexes())
	// 每存储分片数。
	amount := s.OutSize / rest.Span
	// 用户取消行为
	cancel := pputil.Canceller(s.User.ChExit())

	var done bool
	for !done {
		rtch := make(chan int64)
		if cancel() {
			break
		}
		// 分片索引管理服务。
		go s.restManage(rest, rtch)

		for err := range s.serve(amount, rtch) {
			log.Println(err)
		}
		close(rtch)

		// 等待分片索引管理服务退出。
		<-s.rtsem
		if rest.Empty() {
			break
		}
		s.dtch = s.Dler.Run(rest.Span, rest.Indexes())
	}
}

//
// 启动一轮下载服务。
// 针对目标分片定义执行下载任务。
//
// 返回的通道如果传递错误，说明有未下载成功的分片。
// 外部检测该信息，必要时再次开启一轮服务。
//
//  @amount 每批存储的分片数。
//  @rtch 用于传递成功下载分片的索引
//
func (s *Server) serve(amount int, rtch chan<- int64) <-chan error {
	ch := make(chan error)

	// 持续读取下载传递来的数据，
	// 按缓存配置逐批（amount）写入存储。
	go func() {
		buf := make([]PieceData, 0, amount)
		var wg sync.WaitGroup
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
			if len(buf) < amount {
				continue
			}
			// 异步存储
			// 下载不受存储效率影响。
			pbs := buf
			wg.Add(1)
			go func() {
				for err := range saveCache(pbs, rtch) {
					ch <- err
				}
				wg.Done()
			}()
			buf = make([]PieceData, 0, amount)
		}
		// 末尾批次/中断剩余
		// 不必异步。
		if len(buf) > 0 {
			for err := range saveCache(buf, rtch) {
				ch <- err
			}
		}
		wg.Wait()
		// 对外通知结束
		close(ch)

	}()

	return ch
}

//
// 缓存存储到外部。
//
func (s *Server) saveCache(pd []PieceData, ch chan<- int64) <-chan error {
	done := func(k int64) {
		ch <- k // 已存储分片的索引
	}
	cc := NewCacher(pd, s.Outer, done)

	// 取集合大小的一半为并发量，
	// 仅是一个简单的直觉处理。
	limit := len(pd)/2 + 1

	return goes.WorksLong(goes.LimitTasker(&cc, limit), nil)
}

//
// 分片索引管理。
//  - 接收已成功下载的分片id，删除其对应记录；
//  - 定时刷新未下载索引存储；
//  - 即时更新下载状态；
//
func (s *Server) restManage(rest RestPieces, ch <-chan int64) {
	tm := s.Interval
	if tm == 0 {
		tm = IndexInterval
	}
	tick := time.NewTicker(tm)

	for k := range ch {
		select {
		case <-tick:
			rest.Total = len(rest.Sums)
			if _, err := s.Indexer.WriteAt(rest.Bytes(), 0); err != nil {
				log.Println(err)
			}
		default: // through...
		}
		delete(rest.Sums, k)
		s.speed.Add(1)
	}
	tick.Stop()

	// 服务完毕
	s.rtsem <- struct{}{}
}
