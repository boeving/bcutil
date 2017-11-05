package goes_test

import (
	"fmt"

	"github.com/qchen-zh/pputil/goes"
)

type Team []string

// goes.Getter 接口的实现。
func (t Team) IntGet(i int) (v interface{}, ok bool) {
	if len(t) > 0 && i < len(t) {
		v, ok = t[i], true
	}
	return
}

var th = Team{
	"Jon snow",
	"Daenerys Targaryen",
	"Tyrion Lannister",
	"Arya Stark",
	"Bran Stark",
	"Jorah Mormont",
	"Samwell Tarly",
}

func ExampleGets() {
	// 取偶数索引
	ch := goes.Gets(th, 0, 2, nil)

	for v := range ch {
		// 确知v存储一个字符串
		fmt.Println("Hai, " + v.(string))
	}
	fmt.Println("Bye!")

	// Output:
	// Hai, Jon snow
	// Hai, Tyrion Lannister
	// Hai, Bran Stark
	// Hai, Samwell Tarly
	// Bye!
}
