package peerd

// NOTE: THIS FILE WAS PRODUCED BY THE
// MSGP CODE GENERATION TOOL (github.com/tinylib/msgp)
// DO NOT EDIT

import (
	"github.com/tinylib/msgp/msgp"
)

// DecodeMsg implements msgp.Decodable
func (z *Piece) DecodeMsg(dc *msgp.Reader) (err error) {
	var field []byte
	_ = field
	var zb0001 uint32
	zb0001, err = dc.ReadMapHeader()
	if err != nil {
		return
	}
	for zb0001 > 0 {
		zb0001--
		field, err = dc.ReadMapKeyPtr()
		if err != nil {
			return
		}
		switch msgp.UnsafeString(field) {
		case "Begin":
			z.Begin, err = dc.ReadInt64()
			if err != nil {
				return
			}
		case "End":
			z.End, err = dc.ReadInt64()
			if err != nil {
				return
			}
		default:
			err = dc.Skip()
			if err != nil {
				return
			}
		}
	}
	return
}

// EncodeMsg implements msgp.Encodable
func (z Piece) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 2
	// write "Begin"
	err = en.Append(0x82, 0xa5, 0x42, 0x65, 0x67, 0x69, 0x6e)
	if err != nil {
		return err
	}
	err = en.WriteInt64(z.Begin)
	if err != nil {
		return
	}
	// write "End"
	err = en.Append(0xa3, 0x45, 0x6e, 0x64)
	if err != nil {
		return err
	}
	err = en.WriteInt64(z.End)
	if err != nil {
		return
	}
	return
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z Piece) Msgsize() (s int) {
	s = 1 + 6 + msgp.Int64Size + 4 + msgp.Int64Size
	return
}

// DecodeMsg implements msgp.Decodable
func (z *PieceData) DecodeMsg(dc *msgp.Reader) (err error) {
	var field []byte
	_ = field
	var zb0001 uint32
	zb0001, err = dc.ReadMapHeader()
	if err != nil {
		return
	}
	for zb0001 > 0 {
		zb0001--
		field, err = dc.ReadMapKeyPtr()
		if err != nil {
			return
		}
		switch msgp.UnsafeString(field) {
		case "Offset":
			z.Offset, err = dc.ReadInt64()
			if err != nil {
				return
			}
		case "Bytes":
			z.Bytes, err = dc.ReadBytes(z.Bytes)
			if err != nil {
				return
			}
		default:
			err = dc.Skip()
			if err != nil {
				return
			}
		}
	}
	return
}

// EncodeMsg implements msgp.Encodable
func (z *PieceData) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 2
	// write "Offset"
	err = en.Append(0x82, 0xa6, 0x4f, 0x66, 0x66, 0x73, 0x65, 0x74)
	if err != nil {
		return err
	}
	err = en.WriteInt64(z.Offset)
	if err != nil {
		return
	}
	// write "Bytes"
	err = en.Append(0xa5, 0x42, 0x79, 0x74, 0x65, 0x73)
	if err != nil {
		return err
	}
	err = en.WriteBytes(z.Bytes)
	if err != nil {
		return
	}
	return
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z *PieceData) Msgsize() (s int) {
	s = 1 + 7 + msgp.Int64Size + 6 + msgp.BytesPrefixSize + len(z.Bytes)
	return
}
