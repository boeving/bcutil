package server

// NOTE: THIS FILE WAS PRODUCED BY THE
// MSGP CODE GENERATION TOOL (github.com/tinylib/msgp)
// DO NOT EDIT

import (
	"github.com/tinylib/msgp/msgp"
)

// DecodeMsg implements msgp.Decodable
func (z *Args) DecodeMsg(dc *msgp.Reader) (err error) {
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
		case "A":
			z.A, err = dc.ReadInt()
			if err != nil {
				return
			}
		case "B":
			z.B, err = dc.ReadInt()
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
func (z Args) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 2
	// write "A"
	err = en.Append(0x82, 0xa1, 0x41)
	if err != nil {
		return err
	}
	err = en.WriteInt(z.A)
	if err != nil {
		return
	}
	// write "B"
	err = en.Append(0xa1, 0x42)
	if err != nil {
		return err
	}
	err = en.WriteInt(z.B)
	if err != nil {
		return
	}
	return
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z Args) Msgsize() (s int) {
	s = 1 + 2 + msgp.IntSize + 2 + msgp.IntSize
	return
}

// DecodeMsg implements msgp.Decodable
func (z *Quotient) DecodeMsg(dc *msgp.Reader) (err error) {
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
		case "Quo":
			z.Quo, err = dc.ReadInt()
			if err != nil {
				return
			}
		case "Rem":
			z.Rem, err = dc.ReadInt()
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
func (z Quotient) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 2
	// write "Quo"
	err = en.Append(0x82, 0xa3, 0x51, 0x75, 0x6f)
	if err != nil {
		return err
	}
	err = en.WriteInt(z.Quo)
	if err != nil {
		return
	}
	// write "Rem"
	err = en.Append(0xa3, 0x52, 0x65, 0x6d)
	if err != nil {
		return err
	}
	err = en.WriteInt(z.Rem)
	if err != nil {
		return
	}
	return
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z Quotient) Msgsize() (s int) {
	s = 1 + 4 + msgp.IntSize + 4 + msgp.IntSize
	return
}
