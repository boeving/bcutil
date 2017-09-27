package httpd

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

// MaxThread 最大线程数量
var MaxThread = 5

// FileDl 文件下载管理器。
type FileDl struct {
	URL       string   // 下载地址
	Size      int64    // 文件大小
	File      *os.File // 要写入的文件
	BlockList []Block  // 用于记录未下载的文件块起始位置

	onStart  func()
	onPause  func()
	onResume func()
	onCancel func()
	onFinish func()
	onError  func(int, error)

	paused bool
	status Status
}

// NewFileDl 创建新的文件下载
// 如果 size <= 0 则自动获取文件大小
func NewFileDl(url string, file *os.File, size int64) (*FileDl, error) {
	if size <= 0 {
		// 获取文件信息
		resp, err := http.Get(url)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		size = resp.ContentLength
	}

	f := &FileDl{
		URL:  url,
		Size: size,
		File: file,
	}

	return f, nil
}

// Start 开始下载
func (f *FileDl) Start() {
	go func() {
		if f.Size <= 0 {
			f.BlockList = append(f.BlockList, Block{0, -1})
		} else {
			blockSize := f.Size / int64(MaxThread)
			var begin int64
			// 数据平均分配给各个线程
			for i := 0; i < MaxThread; i++ {
				var end = (int64(i) + 1) * blockSize
				f.BlockList = append(f.BlockList, Block{begin, end})
				begin = end + 1
			}
			// 将余出数据分配给最后一个线程
			f.BlockList[MaxThread-1].End += f.Size - f.BlockList[MaxThread-1].End
		}

		f.touch(f.onStart)
		// 开始下载
		err := f.download()
		if err != nil {
			f.touchOnError(0, err)
			return
		}
	}()
}

func (f *FileDl) download() error {
	f.startGetSpeeds()

	ok := make(chan bool, MaxThread)
	for i := range f.BlockList {
		go func(id int) {
			defer func() {
				ok <- true
			}()

			for {
				err := f.downloadBlock(id)
				if err != nil {
					f.touchOnError(0, err)
					// 重新下载
					continue
				}
				break
			}
		}(i)
	}

	for i := 0; i < MaxThread; i++ {
		<-ok
	}
	// 检查是否为暂停导致的“下载完成”
	if f.paused {
		f.touch(f.onPause)
		return nil
	}
	f.paused = true
	f.touch(f.onFinish)

	return nil
}

// 文件块下载器
// 根据线程ID获取下载块的起始位置
func (f *FileDl) downloadBlock(id int) error {
	request, err := http.NewRequest("GET", f.URL, nil)
	if err != nil {
		return err
	}
	begin := f.BlockList[id].Begin
	end := f.BlockList[id].End
	if end != -1 {
		request.Header.Set(
			"Range",
			"bytes="+strconv.FormatInt(begin, 10)+"-"+strconv.FormatInt(end, 10),
		)
	}

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var buf = make([]byte, CacheSize)
	for {
		if f.paused == true {
			// 下载暂停
			return nil
		}

		n, e := resp.Body.Read(buf)

		bufSize := int64(len(buf[:n]))
		if end != -1 {
			// 检查下载的大小是否超出需要下载的大小
			// 这里End+1是因为http的Range的end是包括在需要下载的数据内的
			// 比如 0-1 的长度其实是2，所以这里end需要+1
			needSize := f.BlockList[id].End + 1 - f.BlockList[id].Begin
			if bufSize > needSize {
				// 数据大小不正常
				// 一般是因为网络环境不好导致
				// 比如用中国电信下载国外文件

				// 设置数据大小来去掉多余数据
				// 并结束这个线程的下载
				bufSize = needSize
				n = int(needSize)
				e = io.EOF
			}
		}
		// 将缓冲数据写入硬盘
		f.File.WriteAt(buf[:n], f.BlockList[id].Begin)

		// 更新已下载大小
		f.status.Downloaded += bufSize
		f.BlockList[id].Begin += bufSize

		if e != nil {
			if e == io.EOF {
				// 数据已经下载完毕
				return nil
			}
			return e
		}
	}

	return nil
}

func (f *FileDl) startGetSpeeds() {
	go func() {
		var old = f.status.Downloaded
		for {
			if f.paused {
				f.status.Speeds = 0
				return
			}
			time.Sleep(time.Second * 1)
			f.status.Speeds = f.status.Downloaded - old
			old = f.status.Downloaded
		}
	}()
}

// GetStatus 获取下载统计信息
func (f FileDl) GetStatus() Status {
	return f.status
}

// Pause 暂停下载
func (f *FileDl) Pause() {
	f.paused = true
}

// Resume 继续下载
func (f *FileDl) Resume() {
	f.paused = false
	go func() {
		if f.BlockList == nil {
			f.touchOnError(0, errBlock)
			return
		}

		f.touch(f.onResume)
		err := f.download()
		if err != nil {
			f.touchOnError(0, err)
			return
		}
	}()
}

// OnStart 任务开始时触发的事件
func (f *FileDl) OnStart(fn func()) {
	f.onStart = fn
}

// OnPause 任务暂停时触发的事件
func (f *FileDl) OnPause(fn func()) {
	f.onPause = fn
}

// OnResume 任务继续时触发的事件
func (f *FileDl) OnResume(fn func()) {
	f.onResume = fn
}

// OnFinish 任务完成时触发的事件
func (f *FileDl) OnFinish(fn func()) {
	f.onFinish = fn
}

// OnError 任务出错时触发的事件
// errCode为错误码，errStr为错误描述
func (f *FileDl) OnError(fn func(int, error)) {
	f.onError = fn
}

// 用于触发事件
func (f FileDl) touch(fn func()) {
	if fn != nil {
		go fn()
	}
}

// 触发Error事件
func (f FileDl) touchOnError(errCode int, err error) {
	if f.onError != nil {
		go f.onError(errCode, err)
	}
}