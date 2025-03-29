package websocket

import (
	"bufio"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

var (
	ErrBadURL           = errors.New("websocket: malformed ws URL")
	ErrDuplicateHeaders = errors.New("websocket: duplicate headers aren't allowed")
	ErrHandshake        = errors.New("websocket: error negotiating handshake with peer")
)

type Dialer struct {
	// ReadBufferSize used for size when making bufio read buffers,
	// if not assigned the default buffer size is 4KB.
	ReadBufferSize int

	// Subprotocols is the client's supported protocols in order of prefernce.
	// if no Subprotocols is specified then no protocol is negotiated during handshake.
	Subprotocols []string

	// TlsConfig used when connecting to a secure websocket connection (eg. wss)
	TlsConfig *tls.Config

	// Headers to be sent during initial handshake,
	// headers MUST NOT include any websocket reserved headers.
	Headers http.Header

	// enableCompression is wether to negotiate per-message deflate extension or not.
	CompressionConfig CompressionConfig

	// CookieJar used to hold cookies to be sent during the initial handshake
	// like cookies for auth (sessions, JWT's, ...)
	CookieJar http.CookieJar
}

// Dial is helper function that creates a [Dialer] and dials the websocket connection
func Dial(urlStr string) (*Conn, *http.Response, error) {
	dialer := Dialer{}
	return dialer.Dial(urlStr)
}

// Dial connects to the weboscket url handles the handshake and returns a [*Conn] representing
// a websocket connection or an error if the handshake fails.
//
// Dial also returns the http response from the handshake if you want to do something with it.
func (d *Dialer) Dial(urlStr string) (*Conn, *http.Response, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, nil, err
	}

	// username and password aren't allowed in websocket url
	if u.User != nil {
		return nil, nil, ErrBadURL
	}

	// convert scheme to http equivalent
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	default:
		return nil, nil, ErrBadURL
	}

	// challange key and hash
	key := makeKey()
	keyHash := makeKeyHash(key)

	// http request
	req := http.Request{
		Method:     http.MethodGet,
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Host:       u.Host,
	}

	// check for duplicate required headers,
	// then add client headers
	if d.Headers != nil {
		if checkDuplicateHeaders(d.Headers, []string{
			"Upgrade",
			"Connection",
			"Sec-WebSocket-Key",
			"Sec-WebSocket-Version",
			"Sec-WebSocket-Extensions",
			"Sec-WebSocket-Protocol",
		}) {
			return nil, nil, ErrDuplicateHeaders
		}

		for k, v := range d.Headers {
			req.Header[k] = v
		}
	}
	// add websocket required headers
	req.Header["Upgrade"] = []string{"websocket"}
	req.Header["Connection"] = []string{"Upgrade"}
	req.Header["Sec-WebSocket-Key"] = []string{key}
	req.Header["Sec-WebSocket-Version"] = []string{VERSION}
	if len(d.Subprotocols) > 0 {
		req.Header["Sec-WebSocket-Protocol"] = []string{strings.Join(d.Subprotocols, ", ")}
	}
	if d.CompressionConfig.Enabled {
		req.Header["Sec-WebSocket-Extensions"] = []string{
			makeFlateExtHeader(!d.CompressionConfig.IsContextTakeover, false),
		}
	}

	// add cookies
	if d.CookieJar != nil {
		for _, c := range d.CookieJar.Cookies(u) {
			req.AddCookie(c)
		}
	}

	// dial url
	netConn, err := d.netDial(u)
	if err != nil {
		return nil, nil, err
	}
	// Clean connection if error happens
	defer func() {
		if netConn != nil {
			// Safe because we set the netConn variable to nil before returning.
			netConn.Close()
		}
	}()

	// write handshake
	err = req.Write(netConn)
	if err != nil {
		return nil, nil, err
	}

	// read handshake response
	var br *bufio.Reader
	if d.ReadBufferSize != 0 {
		br = bufio.NewReaderSize(netConn, d.ReadBufferSize)
	} else {
		// default size is 4KB
		br = bufio.NewReader(netConn)
	}
	res, err := http.ReadResponse(br, &req)
	if err != nil {
		return nil, nil, err
	}

	// Check for main required headers
	if res.StatusCode != 101 ||
		!checkHeaderValue(res.Header, "Upgrade", "websocket") ||
		!checkHeaderValue(res.Header, "Connection", "Upgrade") ||
		res.Header.Get("Sec-WebSocket-Accept") != keyHash {
		return nil, nil, ErrHandshake
	}

	// if header exits, it indicates that's the server
	// doesn't support our websocket version.
	resVersion := res.Header.Get("Sec-WebSocket-Version")
	if resVersion != "" {
		return nil, nil, ErrHandshake
	}

	// subprotocol
	subprotocol := res.Header.Get("Sec-WebSocket-Protocol")
	if len(d.Subprotocols) == 0 && subprotocol != "" {
		return nil, nil, ErrHandshake
	}

	// extension
	exts := parseExtHeader(res.Header)
	isFlate, _, isClientNoTakeover := isFlateIsTakeover(exts)
	cc := &CompressionConfig{
		Enabled:              d.CompressionConfig.Enabled,
		IsContextTakeover:    d.CompressionConfig.IsContextTakeover,
		CompressionLevel:     d.CompressionConfig.CompressionLevel,
		CompressionThreshold: d.CompressionConfig.CompressionThreshold,
	}

	if !isFlate {
		cc.Enabled = false
	}
	if d.CompressionConfig.Enabled && isClientNoTakeover {
		cc.IsContextTakeover = false
	}

	conn := newConn(netConn, br, cc, subprotocol, false)

	// Unset netConn
	netConn = nil
	return conn, res, nil
}

func (d *Dialer) netDial(u *url.URL) (net.Conn, error) {
	var dialURL string
	// add hostname
	dialURL += u.Hostname()
	// add port
	dialURL += ":"
	switch {
	case u.Port() != "":
		dialURL += u.Port()
	case u.Scheme == "http":
		dialURL += "80"
	case u.Scheme == "https":
		dialURL += "433"
	}

	// dial the connection
	if u.Scheme == "https" {
		return tls.Dial("tcp", dialURL, d.TlsConfig)
	} else {
		return net.Dial("tcp", dialURL)
	}
}
