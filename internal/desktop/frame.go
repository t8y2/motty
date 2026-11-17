package desktop

import "sync"

// Frame is the picture of a remote desktop as a backend builds it: BGRA pixels
// that the network goroutine draws into and the UI goroutine reads out of, with
// a record of what changed in between. A backend whose decoders have no
// framebuffer of their own embeds it (VNC does) and implements the drawing half
// of its protocol's decoders on it (Update, Fill, Copy); Present is the other
// half of Session. A backend that already owns the picture (RDP keeps it in the
// client's framebuffer) keeps only the changed-rectangle list.
//
// The changed rectangles are kept as a short list. Beyond maxDirty of them they
// collapse into their bounding box, so a flood of small updates costs one larger
// copy and not an unbounded list.
type Frame struct {
	mu            sync.Mutex
	width, height int
	pix           []byte // width*height*4, rows tight
	sized         bool   // dst must be resized before the next update
	dirty         []Rect
	notify        func()
}

const maxDirty = 16

// NewFrame returns a frame that calls notify, without holding any lock, when
// something in it changed. notify may be nil.
func NewFrame(notify func()) *Frame { return &Frame{notify: notify} }

// Size is the current size of the picture.
func (frame *Frame) Size() (width, height int) {
	frame.mu.Lock()
	defer frame.mu.Unlock()
	return frame.width, frame.height
}

// Resize makes the picture width x height, black, and marks all of it changed.
func (frame *Frame) Resize(width, height int) {
	frame.mu.Lock()
	frame.width, frame.height = width, height
	frame.pix = make([]byte, width*height*4)
	frame.sized = true
	frame.dirty = append(frame.dirty[:0], Rect{0, 0, width, height})
	frame.mu.Unlock()
	frame.wake()
}

func (frame *Frame) wake() {
	if frame.notify != nil {
		frame.notify()
	}
}

// mark records a changed rectangle. The caller holds the lock.
func (frame *Frame) mark(rect Rect) {
	if rect.Empty() {
		return
	}
	if len(frame.dirty) >= maxDirty {
		union := rect
		for _, other := range frame.dirty {
			union = union.Union(other)
		}
		frame.dirty = append(frame.dirty[:0], union)
		return
	}
	frame.dirty = append(frame.dirty, rect)
}

// clip returns rect cut to the picture, false if nothing is left.
func (frame *Frame) clip(x, y, width, height int) (Rect, bool) {
	left, top := max(x, 0), max(y, 0)
	right, bottom := min(x+width, frame.width), min(y+height, frame.height)
	if right <= left || bottom <= top {
		return Rect{}, false
	}
	return Rect{left, top, right - left, bottom - top}, true
}

// Update draws BGRA pixels, rows stride bytes apart, at (x, y).
func (frame *Frame) Update(x, y, width, height int, pix []byte, stride int) {
	frame.mu.Lock()
	rect, ok := frame.clip(x, y, width, height)
	if ok && stride >= width*4 && height > 0 && len(pix) >= (height-1)*stride+width*4 {
		for row := rect.Y; row < rect.Y+rect.Height; row++ {
			source := pix[(row-y)*stride+(rect.X-x)*4:][:rect.Width*4]
			copy(frame.pix[(row*frame.width+rect.X)*4:], source)
		}
		frame.mark(rect)
	} else {
		ok = false
	}
	frame.mu.Unlock()
	if ok {
		frame.wake()
	}
}

// Fill paints a rectangle one color.
func (frame *Frame) Fill(x, y, width, height int, blue, green, red byte) {
	frame.mu.Lock()
	rect, ok := frame.clip(x, y, width, height)
	if ok {
		for row := rect.Y; row < rect.Y+rect.Height; row++ {
			line := frame.pix[(row*frame.width+rect.X)*4:][:rect.Width*4]
			for offset := 0; offset < len(line); offset += 4 {
				line[offset], line[offset+1], line[offset+2], line[offset+3] = blue, green, red, 0
			}
		}
		frame.mark(rect)
	}
	frame.mu.Unlock()
	if ok {
		frame.wake()
	}
}

// Copy moves a rectangle within the picture; the two may overlap.
func (frame *Frame) Copy(x, y, width, height, fromX, fromY int) {
	frame.mu.Lock()
	destination, ok := frame.clip(x, y, width, height)
	source, sourceOK := frame.clip(fromX, fromY, width, height)
	if ok && sourceOK && destination.Width == width && destination.Height == height && source.Width == width && source.Height == height {
		rows := func(first, last, step int) {
			for row := first; row != last; row += step {
				from := frame.pix[((fromY+row)*frame.width+fromX)*4:][:width*4]
				copy(frame.pix[((y+row)*frame.width+x)*4:], from)
			}
		}
		// Copy rows in the order that does not overwrite what is yet to be read.
		if y > fromY {
			rows(height-1, -1, -1)
		} else {
			rows(0, height, 1)
		}
		frame.mark(destination)
	} else {
		ok = false
	}
	frame.mu.Unlock()
	if ok {
		frame.wake()
	}
}

// Present implements Session.Present.
func (frame *Frame) Present(dst Presenter) bool {
	frame.mu.Lock()
	defer frame.mu.Unlock()
	if !frame.sized && len(frame.dirty) == 0 {
		return false
	}
	if frame.sized {
		dst.Resize(frame.width, frame.height)
		frame.sized = false
	}
	for _, rect := range frame.dirty {
		dst.Update(rect.X, rect.Y, rect.Width, rect.Height, frame.pix[(rect.Y*frame.width+rect.X)*4:], frame.width*4)
	}
	frame.dirty = frame.dirty[:0]
	return true
}
