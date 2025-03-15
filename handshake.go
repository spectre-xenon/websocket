package websocket

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	VERSION  = "13"
	KEY_GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

// The Upgrader used to validate the handshake
// and upgrade the connection.
type Upgrader struct {
	// The fuction used to validate request origin.
	// Check the origin carefully to prevent cross-site request forgery.
	CheckOrigin func(r *http.Request) bool

	// Subprotocols is the server's supported protocols in order of prefernce.
	// if no Subprotocols is specified then no protocol is negotiated during handshake.
	Subprotocols []string
}

// TODO: add docs
func (u *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request) error {
	code, err := u.upgradeConnection(w, r)
	if err != nil {
		w.Header().Set("Sec-Websocket-Version", VERSION)
		http.Error(w, http.StatusText(code), code)
		return err
	}

	return nil
}

// TODO: add docs
func (u *Upgrader) UpgradeNoResponse(w http.ResponseWriter, r *http.Request) (int, error) {
	code, err := u.upgradeConnection(w, r)
	if err != nil {
		return code, err
	}

	return http.StatusOK, nil
}

// TODO: add docs
func (u *Upgrader) upgradeConnection(w http.ResponseWriter, r *http.Request) (int, error) {
	// Reject methods other than GET
	if r.Method != http.MethodGet {
		return http.StatusBadRequest, fmt.Errorf("websocket: method not allowed: %s", r.Method)
	}

	// Check for main required headers
	ok := checkHeaderValue(r.Header, "Upgrade", "websocket")
	if !ok {
		return http.StatusBadRequest, fmt.Errorf("websocket: missing/mismatched required Upgrade header")
	}
	ok = checkHeaderValue(r.Header, "Connection", "Upgrade")
	if !ok {
		return http.StatusBadRequest, fmt.Errorf("websocket: missing/mismatched required Connection header")
	}

	// Check websocket proto version
	ok = checkHeaderValue(r.Header, "Sec-WebSocket-Version", VERSION)
	if !ok {
		return http.StatusBadRequest, fmt.Errorf("websocket: missing/mismatched Sec-WebSocket-Version header")
	}

	// Do origin check
	var originAllowed bool
	if u.CheckOrigin == nil {
		originAllowed = checkSameOrigin(r)
	} else {
		originAllowed = u.CheckOrigin(r)
	}
	if !originAllowed {
		return http.StatusBadRequest, fmt.Errorf("websocket: client failed Upgrader.CheckOrigin method")
	}

	// Challange key
	key := r.Header.Get("Sec-WebSocket-Key")
	// No challange key
	if key == "" {
		return http.StatusBadRequest, fmt.Errorf("websocket: no Sec-WebSocket-Key header found")
	}
	if !validKey(key) {
		return http.StatusBadRequest, fmt.Errorf("websocket: invalid challange key value")
	}
	// generate new kay hash
	newKey := makeKeyHash(key)

	// Hijack connection
	_, bufrw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("websocket: error while hijacking: %s", err)

	}

	// Build handshake
	handshake := make([]byte, 0)
	// Protocol resourse and success code
	handshake = append(handshake, "HTTP/1.1 101 Switching Protocols\r\n"...)
	// Required headers
	handshake = append(handshake, "Upgrade: websocket\r\nConnection: Upgrade\r\n"...)
	// Challange key
	handshake = append(handshake, fmt.Sprintf("Sec-WebSocket-Accept: %s\r\n", newKey)...)
	// TODO: add subprotocol selection
	// TODO: add extension handling

	// Required empty line
	handshake = append(handshake, "\r\n"...)

	// Write handshake and flush
	bufrw.Writer.Write(handshake)
	bufrw.Writer.Flush()

	// HACK: testing that it works
	for {
		buf := make([]byte, 1024)
		_, err := bufrw.Reader.Read(buf)
		if err != nil {
			break
		}

		fmt.Printf("recived from ws: %08b\n", buf)
	}

	return http.StatusOK, nil
}

// Check if a list of headers exist with thier corresponding values.
// The headers and thier values are case-insensitive
func checkHeaderValue(headers http.Header, h string, v string) bool {

	values := headers.Values(h)
	// header doesn't exists or is empty
	if len(values) == 0 {
		return false
	}

	for _, strList := range values {
		parts := strings.Split(strList, ",")

		for _, str := range parts {
			s := strings.ToLower(strings.TrimSpace(str))

			if s == strings.ToLower(v) {
				return true
			}
		}
	}

	return false
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

// Validates that the challange key is 16 bytes in length when decoded
func validKey(key string) bool {
	if key == "" {
		return false
	}

	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return false
	}
	return len(decoded) == 16
}

func makeKeyHash(key string) string {
	// Make hash
	hasher := sha1.New()
	hasher.Write([]byte(key))
	hasher.Write([]byte(KEY_GUID))

	// Encode to Base64
	return base64.StdEncoding.EncodeToString(hasher.Sum(nil))
}
