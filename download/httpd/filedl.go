// Package httpd 直接网址下载方式的实现。
//
package httpd

import (
	"fmt"
	"net/http"
	"time"

	dl "github.com/qchen-zh/pputil/download"
)

// 下载专用客户端。
// 外部可根据分片大小设置适当超时。
// 默认1分钟。
var Client = &http.Client{
	Timeout: 1 * time.Minute,
}

// FileDl 文件下载器。
// 对 Hauler 和 Getter 接口的实现。
type FileDl struct {
	URL string
}

//
// New 新建一个数据搬运工。
// 返回自身即可，无并发冲突。
//
func (f FileDl) New() dl.Getter {
	return f
}

//
// Get 下载当前分片。
// 如果p.End为零，表示下载整个文件。
//
func (f FileDl) Get(p Piece) ([]byte, error) {
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
	resp, err := Client.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	buf := make([]byte, p.Size())
	_, err = resp.Body.Read(buf)

	return buf, err
}

//
// FileSize 获取URL文件大小。
//
func FileSize(url string) (int64, error) {
	resp, err := Client.Head(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	return resp.ContentLength, nil
}
