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
var flateWriterPool, flateReaderPool sync.Pool

type flateWriter struct {
	fw    *flate.Writer
	level int
}

func getFlateWriter(w io.Writer, level int) *flateWriter {
	fws, ok := flateReaderPool.Get().(*flateWriter)
	if !ok {
		fw, _ := flate.NewWriter(w, level)
		fws = &flateWriter{
			fw: fw,
		}
		return fws
	}
	if fws.level != level {
		putFlateWriter(fws)
		fw, _ := flate.NewWriter(w, level)
		fws = &flateWriter{
			fw: fw,
		}
		return fws
	}

	fws.fw.Reset(w)
	return fws
}

func putFlateWriter(fw *flateWriter) {
	flateReaderPool.Put(fw)
}

func getFlateReader(r io.Reader, dict []byte) io.Reader {
	fr, ok := flateReaderPool.Get().(flate.Reader)
	if !ok {
		return flate.NewReaderDict(r, dict)
	}
	// cast flate.Reader to flate.Resetter
	fr.(flate.Resetter).Reset(nil, nil)
	return fr
}

func putFlateReader(fr io.Reader) {
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
	fws *flateWriter
	fr  io.Reader

	writeBuffer, readBuffer bytes.Buffer
	compressionLevel        int
	// sliding window
	sw *slidingWindow

	isContextTakeover bool
}

func newFlatter(cc *CompressionConfig) *flatter {
	var writeBuffer bytes.Buffer

	// use a known compressionlevel
	if cc.CompressionLevel != flate.DefaultCompression &&
		cc.CompressionLevel != flate.BestSpeed &&
		cc.CompressionLevel != flate.BestCompression {
		cc.CompressionLevel = flate.DefaultCompression
	}

	fws := getFlateWriter(&writeBuffer, cc.CompressionLevel)
	fr := getFlateReader(nil, nil)

	var sw *slidingWindow
	if cc.IsContextTakeover {
		sw = getSlidingWindow()
	}

	return &flatter{
		fws:               fws,
		fr:                fr,
		writeBuffer:       writeBuffer,
		compressionLevel:  cc.CompressionLevel,
		sw:                sw,
		isContextTakeover: cc.IsContextTakeover,
	}
}

func (f *flatter) renewWriter() {
	f.fws.fw.Reset(&f.writeBuffer)
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

	_, err := f.fws.fw.Write(payload)
	if err != nil {
		return nil, err
	}
	err = f.fws.fw.Flush()
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
	putFlateReader(f.fr)
	putFlateWriter(f.fws)
	if f.isContextTakeover {
		putSlidingWindow(f.sw)
	}
}
