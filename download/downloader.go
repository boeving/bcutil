// Package download 下载器。
// 支持并发、断点续传。
// 外部实现特定的下载方式，如http直接下载或P2P传输。
// 由用户定义下载响应集。
//
package download

import (
	"errors"
	"io"

	"github.com/qchen-zh/pputil/download/piece"
)

type (
	Piece      = piece.Piece
	Pieces     = piece.Pieces
	RestPieces = piece.RestPieces
)

const (
	// MaxThread 默认下载Go程数量
	MaxThread = 10
)

var (
	errPieces  = errors.New("the pieces hasn't data")
	errWriteAt = errors.New("the Writer not support WriteAt")
	errIndexer = errors.New("the index Writer is invalid")
	errIndex   = errors.New("the indexs not in blocks")
)

// Status 下载状态。
type Status struct {
	Total     int64
	Completed int64
}

//
// Hauler 数据搬运工。
// 实施单个目标（分片）的具体下载行为，
//
type Hauler interface {
	// 下载分片。
	Start(Piece) error

	// 注册完成回调。
	Completed(func(Piece, []byte))

	// 注册失败回调。
	Failed(func(Piece, error))
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
	Monitor               // 监控器
	Rest    RestPieces    // 未下载分片信息集
	Indexer io.ReadWriter // 未下载分片索引存储
}

//
// IndexInit 分片索引初始化。
// 仅在初始下载时调用，初始化未下载分片信息集。
//
// p 应当已经读取目标文件校验哈希集。
//
func (d *Downloader) IndexInit(p Pieces) error {
	if len(p.Index) == 0 {
		return errPieces
	}
	d.Rest = RestPieces{p}
}

//
// RestLoad 载入待分片索引载入。
// 仅在续传时初始调用，未下载分片索引已经存储。
//
func (d *Downloader) RestLoad() error {
	if d.Indexer == nil {
		return errIndexer
	}
	return d.Rest.Read(d.Indexer)
}

//
// RestPuts 未下载分片索引缓存。
//
func (d *Downloader) RestPuts() (int, error) {
	if len(d.restIndexes) == 0 {
		return 0, nil
	}
	n := 0
	buf := make([]byte, 0, len(d.restIndexes)*lenPieceIdx)

	for off := range d.restIndexes {
		b := d.List[off]
		if b == nil {
			return 0, errIndex
		}
		buf = append(buf, b.Bytes()...)
	}

	return d.Indexer.Write(buf)
}

//
// Download 执行下载。
// limit 为下载积累最大值，到此值后输出下载数据。
// 如果输出成功，内部更新未下载集合。
//
func (d *Downloader) Download(limit int, put func([]byte) bool) error {

}

//
// 分片定义取值渠道。
// 对外传递未下载分片定义{Begin, End}。
//
func (d *Downloader) pieceGetter() <-chan Piece {
	ch := make(chan Piece)

	go func() {
		for off := range d.Rest.Index {
			ch <- Piece{off, off + d.Rest.Span}
		}
		close(ch)
	}()

	return ch
}
