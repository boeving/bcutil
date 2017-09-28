package goes_test

import (
	"fmt"

	"github.com/qchen-zh/pputil/goes"
)

type Team struct {
	members []string
	idx     int
}

// goes.Valuer 接口的实现。
// v存储一个字符串值。
func (t *Team) Value() (v interface{}, ok bool) {
	m := t.members

	if len(m) > 0 && t.idx < len(m) {
		v, ok = m[t.idx], true
		t.idx++
	}
	return
}

func ExampleServe() {
	ts := Team{
		members: []string{
			"Jon snow",
			"Daenerys Targaryen",
			"Tyrion Lannister",
			"Arya Stark",
			"Bran Stark",
			"Jorah Mormont",
			"Samwell Tarly",
		},
	}
	ch := goes.Serve(&ts, nil)
	for {
		v, ok := <-ch
		if !ok {
			break
		}
		// v 的值确定是一个字符串
		fmt.Println("Hai, " + v.(string))
	}
	fmt.Println("Bye!")

	// Output:
	// Hai, Jon snow
	// Hai, Daenerys Targaryen
	// Hai, Tyrion Lannister
	// Hai, Arya Stark
	// Hai, Bran Stark
	// Hai, Jorah Mormont
	// Hai, Samwell Tarly
	// Bye!
}
