package cli

import (
	"errors"
	"io"
)

func isEOF(err error) bool {
	return errors.Is(err, io.EOF)
}
