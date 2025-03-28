package websocket

import (
	"bytes"
	"compress/flate"
	"io"
	"strings"
	"sync"
)

const (
	flateTail =
	// required flate tail in flate.Reader (equivalent to an EOF)
	"\x00\x00\xff\xff" +
		// we add those bytes to prevent ErrUnexpectedEOF from flate.Reader
		"\x01\x00\x00\xff\xff"
)

// we use a sync.Pool to reuse existing flate objects,
// and to make it safe for concurrent use.
var flateReaderPool sync.Pool

func getFlateReader(r io.Reader, dict []byte) io.Reader {
	fr, ok := flateReaderPool.Get().(flate.Reader)
	if !ok {
		return flate.NewReaderDict(r, dict)
	}
	// cast flate.Reader to flate.Resetter
	return fr
}

func putFlateReader(fr io.Reader) {
	fr.(flate.Resetter).Reset(nil, nil)
	flateReaderPool.Put(fr)
}

type slidingWindow struct {
	buf []byte
}

func (sw *slidingWindow) write(p []byte) {
	// If input exceeds buffer capacity, keep only the newest portion
	if len(p) >= cap(sw.buf) {
		// Truncate input to match buf len
		p = p[len(p)-cap(sw.buf):]
		copy(sw.buf, p)
		return
	}

	spaceLeft := cap(sw.buf) - len(sw.buf)
	if len(p) > spaceLeft {
		spaceNeeded := len(p) - spaceLeft
		// Shift existing data left (discard oldest 'spaceNeeded' bytes)
		copy(sw.buf, sw.buf[spaceNeeded:])
		// Truncate buf to remove excess bytes (already shifted to left)
		sw.buf = sw.buf[:len(sw.buf)-spaceNeeded]
	}

	sw.buf = append(sw.buf, p...)
}

var swPool sync.Pool

func getSlidingWindow() *slidingWindow {
	sw, ok := swPool.Get().(*slidingWindow)
	if !ok {
		return &slidingWindow{buf: make([]byte, 32*1024)}
	}
	return sw
}

func putSlidingWindow(sw *slidingWindow) {
	// clear buffer
	sw.buf = sw.buf[:0]
	swPool.Put(sw)
}

type CompressionConfig struct {
	Enabled           bool
	IsContextTakeover bool
	// CompressionLevel is used in the compress/flate package
	// if using contextTakeover the recommended level is [flate.DefaultCompression]
	// to make use of the sliding window
	CompressionLevel     int
	CompressionThreshold int
}

type flatter struct {
	fw *flate.Writer
	fr io.Reader

	writeBuffer, readBuffer bytes.Buffer
	compressionLevel        int
	// sliding window
	sw *slidingWindow

	isContextTakeover bool
}

func NewFlatter(cc *CompressionConfig) *flatter {
	var writeBuffer bytes.Buffer

	// use a known compressionlevel
	if cc.CompressionLevel != flate.DefaultCompression &&
		cc.CompressionLevel != flate.BestSpeed &&
		cc.CompressionLevel != flate.BestCompression {
		cc.CompressionLevel = flate.DefaultCompression
	}

	fw, _ := flate.NewWriterDict(&writeBuffer, cc.CompressionLevel, nil)
	fr := getFlateReader(nil, nil)

	var sw *slidingWindow
	if cc.IsContextTakeover {
		sw = getSlidingWindow()
	}

	return &flatter{
		fw:                fw,
		fr:                fr,
		writeBuffer:       writeBuffer,
		compressionLevel:  cc.CompressionLevel,
		sw:                sw,
		isContextTakeover: cc.IsContextTakeover,
	}
}

func (f *flatter) renewWriter() {
	if f.isContextTakeover {
		f.fw, _ = flate.NewWriterDict(&f.writeBuffer, flate.DefaultCompression, f.sw.buf)
	} else {
		f.fw, _ = flate.NewWriter(&f.writeBuffer, flate.DefaultCompression)
	}
}

func (f *flatter) renewReader(payload []byte) {
	r := io.MultiReader(bytes.NewReader(payload), strings.NewReader(flateTail))

	if f.isContextTakeover {
		f.fr.(flate.Resetter).Reset(r, f.sw.buf)
	} else {
		f.fr.(flate.Resetter).Reset(r, nil)
	}
}

func (f *flatter) DeFlate(payload []byte) ([]byte, error) {
	f.renewWriter()
	f.writeBuffer.Reset()

	_, err := f.fw.Write(payload)
	if err != nil {
		return nil, err
	}
	err = f.fw.Flush()
	if err != nil {
		return nil, err
	}

	writtenBytes := f.writeBuffer.Bytes()
	// remove tail as it's considered excess bytes on the wire
	return writtenBytes[:len(writtenBytes)-4], nil
}

func (f *flatter) InFlate(payload []byte) ([]byte, error) {
	f.renewReader(payload)
	f.readBuffer.Reset()

	_, err := io.Copy(&f.readBuffer, f.fr)
	if err != nil {
		return nil, err
	}

	readBytes := f.readBuffer.Bytes()
	if f.isContextTakeover {
		f.sw.write(readBytes)
	}

	return readBytes, nil
}

func (f *flatter) Close() {
	f.fw.Close()
	putFlateReader(f.fr)
	if f.isContextTakeover {
		putSlidingWindow(f.sw)
	}
}
