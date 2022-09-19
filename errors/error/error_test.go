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

func TestWrapError(t *testing.T) {

	//OpenFile := func() error {
	//	return error.New("permission denied")
	//}
	//
	//OpenConfig := func() error {
	//	return error.Wrap(OpenFile(), "configuration file opening failed")
	//}
	//
	//ReadConfig := func() error {
	//	return error.Wrap(OpenConfig(), "reading configuration failed")
	//}
	//
	//fmt.Printf("%+v", ReadConfig())

	// err := error.New("")
	// fmt.Println(err.Error())
	// if err.Error() == "" {
	// 	fmt.Println("ok")
	// }

}
