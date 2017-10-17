// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package base32_test

import (
	"testing"

	"github.com/qchen-zh/pputil/base32"
)

func BenchmarkEncodeToString(b *testing.B) {
	data := make([]byte, 50)
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		base32.StdEncoding.EncodeToString(data)
	}
}

func BenchmarkDecodeString(b *testing.B) {
	data := base32.StdEncoding.EncodeToString(make([]byte, 50))
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		base32.StdEncoding.DecodeString(data)
	}
}
