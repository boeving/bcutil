// Package download 下载器。
// 支持并发、断点续传。
// 外部实现特定的下载方式，如http直接下载或P2P传输。
// 由用户定义下载响应集。
//
package download

import (
	"github.com/qchen-zh/pputil/download/piece"
	"github.com/qchen-zh/pputil/goes"
)

type (
	Piece  = piece.Piece
	Pieces = piece.Pieces
)

const (
	// MaxThread 默认下载协程数
	MaxThread = 8
)

//
// Hauler 数据搬运工。
// 实施单个目标（分片）的具体下载行为，
//
type Hauler interface {
	// 获取数据。
	Get(Piece) ([]byte, error)
}

//
// PieceData 分片数据（用于存储）。
//
type PieceData struct {
	Offset int64
	Bytes  []byte
}

//
// Downloader 下载器。
// 负责目标数据的分片，组合校验，缓存等。
//
type Downloader struct {
	NewHauler func() Hauler  // 创建数据搬运工
	pich      chan Piece     // 分片配置获取渠道
	dtch      chan PieceData // 数据传递渠道
}

//
// NewDownloader 新建一个下载器。
//
func NewDownloader(fx func() Hauler) *Downloader {
	return &Downloader{NewHauler: fx}
}

//
// Run 执行下载。
// rest 为外部传递的一个待下载分片定义集。
// 返回一个分片数据读取通道。
// 当下载进程完毕后，通道关闭（可能有下载失败）。
//
func (d *Downloader) Run(rest Pieces) <-chan PieceData {
	// 搬运工数量
	max := MaxThread
	if len(rest.Index) < max {
		max = len(rest.Index)
	}
	// max作为通道缓存仅是一种主观处理。
	// 通道的效率与外部存储IO相关。
	d.dtch = make(chan PieceData, max)

	// 分片索引服务
	d.pich = pieceGetter(rest)

	err := goes.WorksLong(goes.LimitTasker(d, max), nil)
	go func() {
		for _ = range err {
			// 忽略下载失败
		}
		close(d.dtch)
	}()

	return d.dtch
}

///////////////////
// Tasker 接口实现
///////////////////////////////////////////////////////////////////////////////

//
// Task 获取一个分片定义。
//
func (d *Downloader) Task() (k interface{}, ok bool) {
	k, ok = d.pich
	return
}

//
// Work 下载单块数据。
// 即时返回，下载失败时无数据传递。
// 数据异步传递，不影响新的工作进程。
//
func (d *Downloader) Work(k interface{}) error {
	p := k.(Piece)
	bs, err := d.NewWork().Get(p)

	if err != nil {
		return err
	}
	// 异步传递解耦。
	go func() {
		d.dtch <- PieceData{p.Begin, bs}
	}()
	return nil
}

//
// 分片定义取值渠道。
// 对外传递未下载分片定义{Begin, End}。
//
func pieceGetter(p Pieces) <-chan Piece {
	ch := make(chan Piece)

	go func() {
		for off := range p.Index {
			ch <- Piece{off, off + int64(p.Span)}
		}
		close(ch)
	}()

	return ch
}
