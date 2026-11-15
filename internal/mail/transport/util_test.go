package transport

import (
	"errors"
	"fmt"
	"net/textproto"
)

func fmtV(v any) string { return fmt.Sprintf("%v %+v %#v", v, v, v) }

func hasTextproto(err error, code int) bool {
	var te *textproto.Error
	return errors.As(err, &te) && te.Code == code
}
