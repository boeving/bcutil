package goes_test

import (
	"fmt"

	"github.com/qchen-zh/pputil/goes"
)

func ExampleClose() {
	ch := goes.NewCloser()

	go func() {
		i := 0
		for i < 5 {
			err := fmt.Errorf("error happened %d", i)
			// non blocking
			ch.Close(err)
			i++
		}
	}()

	i := 0
	for i < 3 {
		err := <-ch.C
		// only the first error
		fmt.Println(err)
		i++
	}
	// Output:
	// error happened 0
	// <nil>
	// <nil>
}
