package error_test

import (
	"fmt"
	"testing"

	"github.com/bitini111/rpcx/errors/code"
	"github.com/bitini111/rpcx/errors/error"
)

func TestNewError(t *testing.T) {
	err := error.NewCode(code.New(10000, "", nil), "My Error")
	fmt.Println(err.Error())
	fmt.Println(error.Code(err))
}

func TestNewNil(t *testing.T) {
	err := error.New("")
	fmt.Println(err.Error())
	if err.Error() == "" {
		fmt.Println("ok")
	}
}
