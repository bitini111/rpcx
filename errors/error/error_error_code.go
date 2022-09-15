package error

import (
	"github.com/bitini111/rpcx/errors/code"
)

// Code returns the error code.
// It returns CodeNil if it has no error code.
func (err *Error) Code() code.Code {
	if err == nil {
		return code.CodeNil
	}
	if err.code == code.CodeNil {
		return Code(err.Unwrap())
	}
	return err.code
}

// SetCode updates the internal code with given code.
func (err *Error) SetCode(code code.Code) {
	if err == nil {
		return
	}
	err.code = code
}
