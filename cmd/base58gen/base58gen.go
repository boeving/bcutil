package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/qchen-zh/pputil/cmd/base58gen/base58"
)

func main() {
	var line string
	input := bufio.NewScanner(os.Stdin) // 扫描器
	for input.Scan() {                  // 迭代扫描，读入下一行，并移除行末的换行符
		line = input.Text()
		if len(line) > 0 {
			break
		}
	}
	fmt.Println(len(line), line)
	ens := base58.Encode([]byte(line))
	fmt.Println(len(ens), ens)
}
