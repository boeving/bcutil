package goes_test

import (
	"fmt"

	"github.com/qchen-zh/pputil/goes"
)

func ExampleClose() {
	ch := goes.NewCloser()

	i := 0
	for i < 5 {
		// no goroutines leak.
		go func(n int) {
			err := fmt.Errorf("error happened %d", n)
			ch.Close(err)
			// non-blocking,
			fmt.Println(err)
		}(i)
		i++
	}

	i = 0
	for i < 3 {
		err := <-ch.C
		// non-blocking,
		// only first error, rest err is nil
		fmt.Println("Outside: ", err)
		i++
	}
}
