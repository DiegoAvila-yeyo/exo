package ptyactor

type ringBuffer struct {
	buf      []byte
	start    int
	length   int
	capacity int
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{
		buf:      make([]byte, capacity),
		capacity: capacity,
	}
}

func (r *ringBuffer) Write(data []byte) {
	if r.capacity == 0 || len(data) == 0 {
		return
	}
	if len(data) >= r.capacity {
		copy(r.buf, data[len(data)-r.capacity:])
		r.start = 0
		r.length = r.capacity
		return
	}

	for _, b := range data {
		if r.length < r.capacity {
			r.buf[(r.start+r.length)%r.capacity] = b
			r.length++
			continue
		}
		r.buf[r.start] = b
		r.start = (r.start + 1) % r.capacity
	}
}

func (r *ringBuffer) Snapshot() []byte {
	if r.length == 0 {
		return nil
	}
	out := make([]byte, r.length)
	if r.start+r.length <= r.capacity {
		copy(out, r.buf[r.start:r.start+r.length])
		return out
	}

	n := copy(out, r.buf[r.start:])
	copy(out[n:], r.buf[:r.length-n])
	return out
}
