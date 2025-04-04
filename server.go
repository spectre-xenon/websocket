package websocket

import (
	"bufio"
	"fmt"
	"net/http"
	"net/url"
	"slices"
)

// The Upgrader used to validate the handshake
// and upgrade the connection.
type Upgrader struct {
	// ReadBufferSize used for size when making bufio read buffers,
	// if not assigned the default buffer size is 4KB.
	ReadBufferSize int

	// The fuction used to validate request origin.
	// Check the origin carefully to prevent cross-site request forgery.
	CheckOrigin func(r *http.Request) bool

	// Subprotocols is the server's supported protocols in order of prefernce.
	// if no Subprotocols is specified then no protocol is negotiated during handshake.
	Subprotocols []string

	// enableCompression is wether to negotiate per-message deflate extension or not.
	CompressionConfig CompressionConfig
}

// checkSameOrigin checks if the origin matchs the host.
// returns True if no origin header was found, it's implied in this case
// that the request was not made from a browser.
func checkSameOrigin(r *http.Request) bool {
	origin := r.Header["Origin"]
	if len(origin) == 0 {
		// No origin header so we can assume the client is not a browser.
		return true
	}
	// parse host from origin and make sure it's valid
	u, err := url.Parse(origin[0])
	if err != nil {
		return false
	}

	return u.Host == r.Host
}

// selectSubprotocol selects a subprotocols from the specified Subprotocols
func (u *Upgrader) selectSubprotocol(headers http.Header) string {
	values := headers.Values("Sec-WebSocket-Protocol")
	subprotocols := splitHeaderValuesBySpace(values)

	for _, protocol := range subprotocols {
		if slices.Contains(u.Subprotocols, protocol) {
			return protocol
		}
	}

	return ""
}

// Upgrade validates incoming http requests for correct webscoket handshake
// and handles any invalid request with a proper http response.
//
// If the handshake was sucessful [Upgrade] returns [*Conn] represnting a websocket connection.
//
// If you want to handle responsing to the http request yourself when the handshake fails
// see [Upgrader.UpgradeNoResponse]
//
// Note that any authenication should be handled before upgrading the connection.
func (u *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	ws, code, err := u.upgradeConnection(w, r)
	if err != nil {
		w.Header().Set("Sec-Websocket-Version", VERSION)
		http.Error(w, http.StatusText(code), code)
		return nil, err
	}

	return ws, nil
}

// UpgradeNoResponse is same as [Upgrader.Upgrade] except it doesn't responed to the http request
// and just returns the recommend http request to responed with.
func (u *Upgrader) UpgradeNoResponse(w http.ResponseWriter, r *http.Request) (*Conn, int, error) {
	ws, code, err := u.upgradeConnection(w, r)
	if err != nil {
		return nil, code, err
	}

	return ws, code, nil
}

// TODO: add docs
func (u *Upgrader) upgradeConnection(w http.ResponseWriter, r *http.Request) (*Conn, int, error) {
	// Reject methods other than GET
	if r.Method != http.MethodGet {
		return nil, http.StatusBadRequest, fmt.Errorf("websocket: method not allowed: %s", r.Method)
	}

	// Check for main required headers
	ok := checkHeaderValue(r.Header, "Upgrade", "websocket")
	if !ok {
		return nil, http.StatusBadRequest, fmt.Errorf("websocket: missing/mismatched required Upgrade header")
	}
	ok = checkHeaderValue(r.Header, "Connection", "Upgrade")
	if !ok {
		return nil, http.StatusBadRequest, fmt.Errorf("websocket: missing/mismatched required Connection header")
	}

	// Check websocket proto version
	ok = checkHeaderValue(r.Header, "Sec-WebSocket-Version", VERSION)
	if !ok {
		return nil, http.StatusBadRequest, fmt.Errorf("websocket: missing/mismatched Sec-WebSocket-Version header")
	}

	// Do origin check
	var originAllowed bool
	if u.CheckOrigin == nil {
		originAllowed = checkSameOrigin(r)
	} else {
		originAllowed = u.CheckOrigin(r)
	}
	if !originAllowed {
		return nil, http.StatusBadRequest, fmt.Errorf("websocket: client failed Upgrader.CheckOrigin method")
	}

	// Challange key
	key := r.Header.Get("Sec-WebSocket-Key")
	// No challange key
	if key == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("websocket: no Sec-WebSocket-Key header found")
	}
	if !isValidKey(key) {
		return nil, http.StatusBadRequest, fmt.Errorf("websocket: invalid challange key value")
	}
	// generate new kay hash
	newKey := makeKeyHash(key)

	// Select a subprotocol (if exists)
	subprotocol := u.selectSubprotocol(r.Header)

	exts := parseExtHeader(r.Header)
	isFlate, isServerNoTakeover, isClientNoTakeover := isFlateIsTakeover(exts)
	cc := &CompressionConfig{
		Enabled:              u.CompressionConfig.Enabled,
		IsContextTakeover:    u.CompressionConfig.IsContextTakeover,
		CompressionLevel:     u.CompressionConfig.CompressionLevel,
		CompressionThreshold: u.CompressionConfig.CompressionThreshold,
	}
	if u.CompressionConfig.Enabled && isServerNoTakeover {
		cc.IsContextTakeover = false
	}

	// Hijack connection
	netConn, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("websocket: error while hijacking: %s", err)
	}
	// Clean connection if error happens
	defer func() {
		if netConn != nil {
			// Safe because we set the netConn variable to nil before returning.
			netConn.Close()
		}
	}()

	// Build handshake
	handshake := make([]byte, 0)
	// Protocol resourse and success code
	handshake = append(handshake, "HTTP/1.1 101 Switching Protocols\r\n"...)
	// Required headers
	handshake = append(handshake, "Upgrade: websocket\r\nConnection: Upgrade\r\n"...)
	// Challange key
	handshake = append(handshake, fmt.Sprintf("Sec-WebSocket-Accept: %s\r\n", newKey)...)
	// selected subprotocol
	if subprotocol != "" {
		handshake = append(handshake, fmt.Sprintf("Sec-WebSocket-Protocol: %s\r\n", subprotocol)...)
	}
	if u.CompressionConfig.Enabled && isFlate {
		ext := makeFlateExtHeader(isServerNoTakeover, isClientNoTakeover)
		handshake = append(handshake, "Sec-WebSocket-Extensions: "+ext...)
	} else {
		cc.Enabled = false
	}

	// Required empty line
	handshake = append(handshake, "\r\n"...)

	// Write handshake directly
	netConn.Write(handshake)

	// create buffer reader
	var br *bufio.Reader
	if u.ReadBufferSize != 0 {
		br = bufio.NewReaderSize(netConn, u.ReadBufferSize)
	} else {
		// default size is 4KB
		br = bufio.NewReader(netConn)
	}

	conn := newConn(netConn, br, cc, subprotocol, true)

	// Unset netConn
	netConn = nil
	return conn, http.StatusSwitchingProtocols, nil
}
