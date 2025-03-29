package websocket

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"strings"
)

const (
	VERSION  = "13"
	KEY_GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

// checkHeaderValue Check if a list of headers exist with thier corresponding values.
// The headers and thier values are case-insensitive.
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

// TODO: add docs
func splitHeaderValuesBySpace(strList []string) []string {
	var splitted []string
	for _, str := range strList {
		parts := strings.Fields(str)

		for _, s := range parts {
			cleaned := strings.TrimSpace(s)
			splitted = append(splitted, cleaned)
		}
	}

	return splitted
}

// TODO: add docs
func checkDuplicateHeaders(headers http.Header, toCheck []string) bool {
	for _, h := range toCheck {
		values := headers.Values(h)
		if len(values) > 0 {
			return true
		}
	}
	return false
}

type extension struct {
	name   string
	params []string
}

func parseExtHeader(h http.Header) []extension {
	exts := make([]extension, 0)

	headerValue := h.Get("Sec-WebSocket-Extensions")
	if headerValue == "" {
		return exts
	}

	extsStr := strings.Split(headerValue, ",")
	// trim space
	for i := range extsStr {
		extsStr[i] = strings.TrimSpace(extsStr[i])
	}

	for _, extStr := range extsStr {
		extParams := strings.Split(extStr, ";")
		// trim space
		for i := range extParams {
			extParams[i] = strings.TrimSpace(extParams[i])
		}

		name := extParams[0]
		var ext extension
		if len(extParams) == 1 {
			ext = extension{
				name: name,
			}
		} else {
			ext = extension{
				name:   name,
				params: extParams[1:],
			}
		}

		exts = append(exts, ext)
	}

	return exts
}

func isFlateIsTakeover(exts []extension) (bool, bool, bool) {
	// check if defalte extension exits and if we're using context_takeover,
	// we don't check for max bit cause we can't adjust defalte window.
exts:
	for _, ext := range exts {
		isServerNoTakeover := false
		isClientNoTakeover := false

		if ext.name != "permessage-deflate" {
			continue
		}

		for _, p := range ext.params {
			switch {
			case p == "client_no_context_takeover":
				isServerNoTakeover = true
				continue
			case p == "server_no_context_takeover":
				isClientNoTakeover = true
				continue
			case p == "server_max_window_bits=15" || p == "client_max_window_bits":
				continue
			case strings.HasPrefix(p, "client_max_window_bits="):
				continue
			default:
				continue exts
			}
		}
		return true, isServerNoTakeover, isClientNoTakeover
	}
	return false, false, false
}

func makeFlateExtHeader(isServerNoTakeover, isClientNoTakeover bool) string {
	ext := "permessage-deflate"
	if isServerNoTakeover {
		ext += "; client_no_context_takeover"
	}
	if isClientNoTakeover {
		ext += "; server_no_context_takeover"
	}
	ext += "\r\n"
	return ext
}

func makeKey() string {
	challangeKey := make([]byte, 16)
	// Never returns an error
	_, _ = rand.Read(challangeKey)
	return base64.StdEncoding.EncodeToString(challangeKey)
}

// Validates that the challange key is 16 bytes in length when decoded.
func isValidKey(key string) bool {
	if key == "" {
		return false
	}

	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return false
	}
	return len(decoded) == 16
}

// TODO: add docs
func makeKeyHash(key string) string {
	// Make hash
	hasher := sha1.New()
	hasher.Write([]byte(key))
	hasher.Write([]byte(KEY_GUID))

	// Encode to Base64
	return base64.StdEncoding.EncodeToString(hasher.Sum(nil))
}
