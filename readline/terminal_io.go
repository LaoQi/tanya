package readline

import (
	"io"
	"syscall"
)

type chunkReader interface {
	readChunk(p []byte) (int, error)
}

type hangUpDetector interface {
	hungUp() bool
}

type keySource struct {
	src    chunkReader
	parser keyParser
	queue  []KeyEvent
}

func (k *keySource) reset() {
	k.queue = nil
	k.parser = keyParser{}
}

func (k *keySource) readKey() (KeyEvent, error) {
	if len(k.queue) > 0 {
		ev := k.queue[0]
		k.queue = k.queue[1:]
		return ev, nil
	}
	buf := make([]byte, 256)
	for {
		n, err := k.src.readChunk(buf)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			if err == syscall.EIO {
				return KeyEvent{}, io.EOF
			}
			return KeyEvent{}, err
		}
		if n > 0 {
			k.queue = append(k.queue, k.parser.feed(buf[:n])...)
		}
		if k.parser.needsMore() && n > 0 {
			continue
		}
		if k.parser.needsMore() && n == 0 {
			k.queue = append(k.queue, k.parser.flush()...)
			break
		}
		if len(k.queue) > 0 {
			break
		}
		if h, ok := k.src.(hangUpDetector); ok && h.hungUp() {
			return KeyEvent{}, io.EOF
		}
	}
	ev := k.queue[0]
	k.queue = k.queue[1:]
	return ev, nil
}
