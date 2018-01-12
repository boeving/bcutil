package main

import (
	"bufio"
	"fmt"
	"os"
        //"encoding/base32"

	"github.com/qchen-zh/pputil/cb32"
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
	dec, _ := cb32.StdEncoding.DecodeString(line)
	fmt.Println(len(dec), string(dec))
}

//KRQWWZJANF4CAZDPO7XCA7DPEB4GQZJAMNXXA8JAMNSW67DFOIQGC5TEEBWWC45FEBQXGIDNMFXHSIDDN7YGSZLTEBQXGIDZN74SA75BNZ4A
//KRQWWZJANF2CAZDPO5XCA5DPEB2GQZJAMNXXA6JAMNSW45DFOIQGC3TEEBWWC23FEBQXGIDNMFXHSIDDN5YGSZLTEBQXGIDZN52SA53BNZ2A====
