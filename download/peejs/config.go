// Package peejs P2P方式分片下载。
// 由端点通过msgp方式传输分片数据，标准rpc（net/rpc）支持。
package peejs

//
// Piece 分片（RPC请求参数）。
// 结构与piece.Piece相同但完整定义，便于msgp单独编码。
//
type Piece struct {
	Begin int64
	End   int64
}

//
// PieceData 分片数据
// （RPC响应数据）。
//
type PieceData struct {
	Offset int64
	Bytes  []byte
}

//
// Size 分片大小。
// 与 piece.Piece 定义相同（此为msgp定制版）。
//
func (p Piece) Size() int {
	return int(p.End - p.Begin)
}
