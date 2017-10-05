// Package httpd 直接网址下载方式的实现。
//
package httpd

import (
	"fmt"
	"net/http"

	dl "github.com/qchen-zh/pputil/download"
)

// FileDl 文件下载器。
// 对 Hauler 接口的实现。
type FileDl struct {
	URL string
}

//
// New 新建一个数据搬运工。
//
func New(url string) dl.Hauler {
	return &FileDl{url}
}

//
// FileSize 获取URL文件大小。
//
func FileSize(url string) (int64, error) {
	resp, err := http.Head(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	return resp.ContentLength, nil
}

//
// Get 下载当前分片。
// 如果p.End为零，表示下载整个文件。
//
func (f *FileDl) Get(p Piece) ([]byte, error) {
	request, err := http.NewRequest("GET", f.URL, nil)
	if err != nil {
		return nil, err
	}
	if p.End > 0 {
		request.Header.Set(
			"Range",
			// End包括在下载的数据内，故-1
			fmt.Sprintf("bytes=%d-%d", p.Begin, p.End-1),
		)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	buf := make([]byte, p.Size())
	_, err = resp.Body.Read(buf)

	return buf, err
}
