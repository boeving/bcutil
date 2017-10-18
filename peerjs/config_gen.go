package peerjs

// NOTE: THIS FILE WAS PRODUCED BY THE
// MSGP CODE GENERATION TOOL (github.com/tinylib/msgp)
// DO NOT EDIT

import (
	"github.com/tinylib/msgp/msgp"
)

// DecodeMsg implements msgp.Decodable
func (z *Request) DecodeMsg(dc *msgp.Reader) (err error) {
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
		case "ServiceMethod":
			z.ServiceMethod, err = dc.ReadString()
			if err != nil {
				return
			}
		case "Seq":
			z.Seq, err = dc.ReadUint64()
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
func (z Request) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 2
	// write "ServiceMethod"
	err = en.Append(0x82, 0xad, 0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x4d, 0x65, 0x74, 0x68, 0x6f, 0x64)
	if err != nil {
		return err
	}
	err = en.WriteString(z.ServiceMethod)
	if err != nil {
		return
	}
	// write "Seq"
	err = en.Append(0xa3, 0x53, 0x65, 0x71)
	if err != nil {
		return err
	}
	err = en.WriteUint64(z.Seq)
	if err != nil {
		return
	}
	return
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z Request) Msgsize() (s int) {
	s = 1 + 14 + msgp.StringPrefixSize + len(z.ServiceMethod) + 4 + msgp.Uint64Size
	return
}

// DecodeMsg implements msgp.Decodable
func (z *Response) DecodeMsg(dc *msgp.Reader) (err error) {
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
		case "ServiceMethod":
			z.ServiceMethod, err = dc.ReadString()
			if err != nil {
				return
			}
		case "Seq":
			z.Seq, err = dc.ReadUint64()
			if err != nil {
				return
			}
		case "Error":
			z.Error, err = dc.ReadString()
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
func (z Response) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 3
	// write "ServiceMethod"
	err = en.Append(0x83, 0xad, 0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x4d, 0x65, 0x74, 0x68, 0x6f, 0x64)
	if err != nil {
		return err
	}
	err = en.WriteString(z.ServiceMethod)
	if err != nil {
		return
	}
	// write "Seq"
	err = en.Append(0xa3, 0x53, 0x65, 0x71)
	if err != nil {
		return err
	}
	err = en.WriteUint64(z.Seq)
	if err != nil {
		return
	}
	// write "Error"
	err = en.Append(0xa5, 0x45, 0x72, 0x72, 0x6f, 0x72)
	if err != nil {
		return err
	}
	err = en.WriteString(z.Error)
	if err != nil {
		return
	}
	return
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z Response) Msgsize() (s int) {
	s = 1 + 14 + msgp.StringPrefixSize + len(z.ServiceMethod) + 4 + msgp.Uint64Size + 6 + msgp.StringPrefixSize + len(z.Error)
	return
}
