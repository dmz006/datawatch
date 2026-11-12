package adapters

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"time"
)

// scanLines feeds each complete, non-empty line to fn and advances
// ctx.Offset past every complete line. A trailing partial line is not
// consumed so the next Parse call sees it whole.
func scanLines(r io.Reader, ctx *Context, fn func(line []byte)) error {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		ctx.Offset += int64(len(line))
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			fn(line)
		}
	}
}

// unixTime converts a unix timestamp in seconds or milliseconds (told
// apart by magnitude) to a time; zero input yields the zero time.
func unixTime(v float64) time.Time {
	switch {
	case v <= 0:
		return time.Time{}
	case v > 1e11:
		return time.UnixMilli(int64(v)).UTC()
	}
	return time.Unix(int64(v), 0).UTC()
}
