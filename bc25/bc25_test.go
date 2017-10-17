// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bc25_test

import (
	"testing"

	"github.com/qchen-zh/pputil/bc25"
)

func BenchmarkEncode(b *testing.B) {
	data := make([]byte, 50)
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		bc25.Encode(data)
	}
}

func BenchmarkDecode(b *testing.B) {
	data := bc25.Encode(make([]byte, 50))
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		bc25.Decode(data)
	}
}
