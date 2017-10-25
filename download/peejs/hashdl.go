package peejs

import (
	"errors"
	"net/rpc"

	"github.com/qchen-zh/pputil/download"
	"github.com/qchen-zh/pputil/download/piece"
)

// 数据传输错误。
// 命名错误，可用于目标端点的评估。
var (
	ErrPiece     = errors.New("the piece(end point) is invalid")
	ErrEmpty     = errors.New("no data on server")
	ErrPieceData = errors.New("the piece data is invalid")
)

// HashDl 通过哈希标识下载。
// 对 Hauler 和 Getter 接口的实现。
type HashDl struct {
	*rpc.Client
}

//
// NewHauler 新建一个数据搬运工。
// 实现 download.Hauler 接口。
// 返回自身即可，仅读取，无并发冲突。
//
func (h HashDl) NewHauler() download.Getter {
	return h
}

//
// Get 下载当前分片。
// 实现 download.Getter 接口。
// 如果p.End不大于零则无效（P2P方式不传递文件整体）。
//
// 返回不同类型的错误，可用于评估目标端点价值。
//
func (h HashDl) Get(p piece.Piece) ([]byte, error) {
	if p.End <= 0 {
		return nil, ErrPiece
	}
	buf := PieceData{}
	// RPC Call
	err := h.Call("DlServer.Piece", Piece(p), &buf)

	if err != nil {
		return nil, err // ErrEmpty
	}
	if buf.Offset != p.Begin || len(buf.Bytes) != p.Size() {
		return nil, ErrPieceData
	}
	return buf.Bytes, err
}

//
// DlServer P2P下载服务端。
// 支持其它端点对分片数据的下载请求（RPC）。
// Cache 对应下载目标的一个既有缓存，数据源，不可为空。
//
// 外部开启RPC服务（net/rpc）并注册一个实例后即可使用。
//
type DlServer struct {
	*download.Cache
}

//
// Piece 获取下载分片数据。
// 支持相同起点偏移的小分片传递（相同文件但分片规划并不相同时）。
//
func (ds DlServer) Piece(args Piece, reply *PieceData) error {
	pd := ds.Get(args.Begin)

	if pd == nil ||
		pd.Offset != args.Begin || len(pd.Bytes) < args.Size() {
		return ErrEmpty
	}
	reply.Offset = pd.Offset
	reply.Bytes = pd.Bytes[:args.Size()]

	return nil
}
