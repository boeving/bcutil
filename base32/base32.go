// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package base32 专用于区块链地址的Base32编码。
// 引用标准库encoding/base32源码，删除了其中Padding相关的代码（即不支持填充字符）。
package base32

import (
	"bytes"
	"io"
	"strconv"
	"strings"
)

/*
 * Encodings
 */

// An Encoding is a radix 32 encoding/decoding scheme, defined by a
// 32-character alphabet.
type Encoding struct {
	encode    string
	decodeMap [256]byte
}

// 标准字符序列。
// 便于外观和读音区分（视觉/听觉）。
// 6s9l9zf4r5s4jfxiu8boleum7cx5x4m9wpsyzoxfrcdo9pei7ctyn4fhrex8lefm7ctyt576rhrybaq
// [a-z4-9]
const EncodeStd = "abcdefjhijklmnopqrstuvwxyz456789"

//
// 选取原则：
//  1. 外观差异较大，便于视觉区分；
//  2. 大小写字符均衡，小写字母向下延展的优先（如gjqy）；
//
// 大写字母17个，小写字母15个。未使用数字。
// wjbNTrQGeHbtuFzGhyGeCRzEmChSzTEQwjqKzrQGeHbtuFzGhyGeCRTEmChSHAECwtSNhrFxmzerLNHGbjqSPPQmwSAKE
const EncodeNice = "ABCDEFGHKLNPQRSTVabeghjmqrtuwxyz"

// NewEncoding returns a new Encoding defined by the given alphabet,
// which must be a 32-byte string.
func NewEncoding(encoder string) *Encoding {
	e := new(Encoding)
	e.encode = encoder

	for i := 0; i < len(e.decodeMap); i++ {
		e.decodeMap[i] = 0xFF
	}
	for i := 0; i < len(encoder); i++ {
		e.decodeMap[encoder[i]] = byte(i)
	}
	return e
}

// 标准编码（含数字）。
var StdEncoding = NewEncoding(EncodeStd)

// 纯字母编码。
var NiceEncoding = NewEncoding(EncodeNice)

var removeNewlinesMapper = func(r rune) rune {
	if r == '\r' || r == '\n' {
		return -1
	}
	return r
}

/*
 * Encoder
 */

// Encode encodes src using the encoding enc, writing
// EncodedLen(len(src)) bytes to dst.
//
// The encoding pads the output to a multiple of 8 bytes,
// so Encode is not appropriate for use on individual blocks
// of a large data stream. Use NewEncoder() instead.
func (enc *Encoding) Encode(dst, src []byte) {
	if len(src) == 0 {
		return
	}

	for len(src) > 0 {
		var b [8]byte

		// Unpack 8x 5-bit source blocks into a 5 byte
		// destination quantum
		switch len(src) {
		default:
			b[7] = src[4] & 0x1F
			b[6] = src[4] >> 5
			fallthrough
		case 4:
			b[6] |= (src[3] << 3) & 0x1F
			b[5] = (src[3] >> 2) & 0x1F
			b[4] = src[3] >> 7
			fallthrough
		case 3:
			b[4] |= (src[2] << 1) & 0x1F
			b[3] = (src[2] >> 4) & 0x1F
			fallthrough
		case 2:
			b[3] |= (src[1] << 4) & 0x1F
			b[2] = (src[1] >> 1) & 0x1F
			b[1] = (src[1] >> 6) & 0x1F
			fallthrough
		case 1:
			b[1] |= (src[0] << 2) & 0x1F
			b[0] = src[0] >> 3
		}

		// Encode 5-bit blocks using the base32 alphabet
		for i := 0; i < 8; i++ {
			if len(dst) > i {
				dst[i] = enc.encode[b[i]]
			}
		}

		// the final quantum
		if len(src) < 5 {
			break
		}

		src = src[5:]
		dst = dst[8:]
	}
}

// EncodeToString returns the base32 encoding of src.
func (enc *Encoding) EncodeToString(src []byte) string {
	buf := make([]byte, enc.EncodedLen(len(src)))
	enc.Encode(buf, src)
	return string(buf)
}

type encoder struct {
	err  error
	enc  *Encoding
	w    io.Writer
	buf  [5]byte    // buffered data waiting to be encoded
	nbuf int        // number of bytes in buf
	out  [1024]byte // output buffer
}

func (e *encoder) Write(p []byte) (n int, err error) {
	if e.err != nil {
		return 0, e.err
	}

	// Leading fringe.
	if e.nbuf > 0 {
		var i int
		for i = 0; i < len(p) && e.nbuf < 5; i++ {
			e.buf[e.nbuf] = p[i]
			e.nbuf++
		}
		n += i
		p = p[i:]
		if e.nbuf < 5 {
			return
		}
		e.enc.Encode(e.out[0:], e.buf[0:])
		if _, e.err = e.w.Write(e.out[0:8]); e.err != nil {
			return n, e.err
		}
		e.nbuf = 0
	}

	// Large interior chunks.
	for len(p) >= 5 {
		nn := len(e.out) / 8 * 5
		if nn > len(p) {
			nn = len(p)
			nn -= nn % 5
		}
		e.enc.Encode(e.out[0:], p[0:nn])
		if _, e.err = e.w.Write(e.out[0 : nn/5*8]); e.err != nil {
			return n, e.err
		}
		n += nn
		p = p[nn:]
	}

	// Trailing fringe.
	for i := 0; i < len(p); i++ {
		e.buf[i] = p[i]
	}
	e.nbuf = len(p)
	n += len(p)
	return
}

// Close flushes any pending output from the encoder.
// It is an error to call Write after calling Close.
func (e *encoder) Close() error {
	// If there's anything left in the buffer, flush it out
	if e.err == nil && e.nbuf > 0 {
		e.enc.Encode(e.out[0:], e.buf[0:e.nbuf])
		e.nbuf = 0
		_, e.err = e.w.Write(e.out[0:8])
	}
	return e.err
}

// NewEncoder returns a new base32 stream encoder. Data written to
// the returned writer will be encoded using enc and then written to w.
// Base32 encodings operate in 5-byte blocks; when finished
// writing, the caller must Close the returned encoder to flush any
// partially written blocks.
func NewEncoder(enc *Encoding, w io.Writer) io.WriteCloser {
	return &encoder{enc: enc, w: w}
}

// EncodedLen returns the length in bytes of the base32 encoding
// of an input buffer of length n.
func (enc *Encoding) EncodedLen(n int) int {
	return (n*8 + 4) / 5
}

/*
 * Decoder
 */

type CorruptInputError int64

func (e CorruptInputError) Error() string {
	return "illegal base32 data at input byte " + strconv.FormatInt(int64(e), 10)
}

// decode is like Decode but returns an additional 'end' value, which
// indicates if end-of-message padding was encountered and thus any
// additional data is an error. This method assumes that src has been
// stripped of all supported whitespace ('\r' and '\n').
func (enc *Encoding) decode(dst, src []byte) (n int, end bool, err error) {
	olen := len(src)
	for len(src) > 0 && !end {
		// Decode quantum using the base32 alphabet
		var dbuf [8]byte
		dlen := 8

		for j := 0; j < 8; {

			// We have reached the end and are not expecing any padding
			if len(src) == 0 {
				dlen, end = j, true
				break
			}
			in := src[0]
			src = src[1:]

			dbuf[j] = enc.decodeMap[in]
			if dbuf[j] == 0xFF {
				return n, false, CorruptInputError(olen - len(src) - 1)
			}
			j++
		}

		// Pack 8x 5-bit source blocks into 5 byte destination
		// quantum
		switch dlen {
		case 8:
			dst[4] = dbuf[6]<<5 | dbuf[7]
			fallthrough
		case 7:
			dst[3] = dbuf[4]<<7 | dbuf[5]<<2 | dbuf[6]>>3
			fallthrough
		case 5:
			dst[2] = dbuf[3]<<4 | dbuf[4]>>1
			fallthrough
		case 4:
			dst[1] = dbuf[1]<<6 | dbuf[2]<<1 | dbuf[3]>>4
			fallthrough
		case 2:
			dst[0] = dbuf[0]<<3 | dbuf[1]>>2
		}

		if !end {
			dst = dst[5:]
		}

		switch dlen {
		case 2:
			n += 1
		case 4:
			n += 2
		case 5:
			n += 3
		case 7:
			n += 4
		case 8:
			n += 5
		}
	}
	return n, end, nil
}

// Decode decodes src using the encoding enc. It writes at most
// DecodedLen(len(src)) bytes to dst and returns the number of bytes
// written. If src contains invalid base32 data, it will return the
// number of bytes successfully written and CorruptInputError.
// New line characters (\r and \n) are ignored.
func (enc *Encoding) Decode(dst, src []byte) (n int, err error) {
	src = bytes.Map(removeNewlinesMapper, src)
	n, _, err = enc.decode(dst, src)
	return
}

// DecodeString returns the bytes represented by the base32 string s.
func (enc *Encoding) DecodeString(s string) ([]byte, error) {
	s = strings.Map(removeNewlinesMapper, s)
	dbuf := make([]byte, enc.DecodedLen(len(s)))
	n, _, err := enc.decode(dbuf, []byte(s))
	return dbuf[:n], err
}

type decoder struct {
	err    error
	enc    *Encoding
	r      io.Reader
	end    bool       // saw end of message
	buf    [1024]byte // leftover input
	nbuf   int
	out    []byte // leftover decoded output
	outbuf [1024 / 8 * 5]byte
}

func readEncodedData(r io.Reader, buf []byte, min int) (n int, err error) {
	for n < min && err == nil {
		var nn int
		nn, err = r.Read(buf[n:])
		n += nn
	}
	if n < min && n > 0 && err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return
}

func (d *decoder) Read(p []byte) (n int, err error) {
	// Use leftover decoded output from last read.
	if len(d.out) > 0 {
		n = copy(p, d.out)
		d.out = d.out[n:]
		if len(d.out) == 0 {
			return n, d.err
		}
		return n, nil
	}

	if d.err != nil {
		return 0, d.err
	}

	// Read a chunk.
	nn := len(p) / 5 * 8
	if nn < 8 {
		nn = 8
	}
	if nn > len(d.buf) {
		nn = len(d.buf)
	}

	nn, d.err = readEncodedData(d.r, d.buf[d.nbuf:nn], 8-d.nbuf)
	d.nbuf += nn
	if d.nbuf < 8 {
		return 0, d.err
	}

	// Decode chunk into p, or d.out and then p if p is too small.
	nr := d.nbuf / 8 * 8
	nw := d.nbuf / 8 * 5
	if nw > len(p) {
		nw, d.end, err = d.enc.decode(d.outbuf[0:], d.buf[0:nr])
		d.out = d.outbuf[0:nw]
		n = copy(p, d.out)
		d.out = d.out[n:]
	} else {
		n, d.end, err = d.enc.decode(p, d.buf[0:nr])
	}
	d.nbuf -= nr
	for i := 0; i < d.nbuf; i++ {
		d.buf[i] = d.buf[i+nr]
	}

	if err != nil && (d.err == nil || d.err == io.EOF) {
		d.err = err
	}

	if len(d.out) > 0 {
		// We cannot return all the decoded bytes to the caller in this
		// invocation of Read, so we return a nil error to ensure that Read
		// will be called again.  The error stored in d.err, if any, will be
		// returned with the last set of decoded bytes.
		return n, nil
	}

	return n, d.err
}

type newlineFilteringReader struct {
	wrapped io.Reader
}

func (r *newlineFilteringReader) Read(p []byte) (int, error) {
	n, err := r.wrapped.Read(p)
	for n > 0 {
		offset := 0
		for i, b := range p[0:n] {
			if b != '\r' && b != '\n' {
				if i != offset {
					p[offset] = b
				}
				offset++
			}
		}
		if err != nil || offset > 0 {
			return offset, err
		}
		// Previous buffer entirely whitespace, read again
		n, err = r.wrapped.Read(p)
	}
	return n, err
}

// NewDecoder constructs a new base32 stream decoder.
func NewDecoder(enc *Encoding, r io.Reader) io.Reader {
	return &decoder{enc: enc, r: &newlineFilteringReader{r}}
}

// DecodedLen returns the maximum length in bytes of the decoded data
// corresponding to n bytes of base32-encoded data.
func (enc *Encoding) DecodedLen(n int) int {
	return n * 5 / 8
}
