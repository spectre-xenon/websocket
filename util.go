package websocket

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"strings"
)

const (
	VERSION  = "13"
	KEY_GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

func makeMaskingKey() []byte {
	maskingKey := make([]byte, 4)
	// Never returns an error
	_, _ = rand.Read(maskingKey)
	return maskingKey
}

func toggleMask(payload, maskingKey []byte) {
	// make uint32 of the MaskingKey
	mask32 := binary.BigEndian.Uint32(maskingKey)
	// as the length of the MaskingKey is 4  we can process
	// process 4 bytes at once (eg. words)
	wordLen := 4
	payloadLen := len(payload)
	numWords := payloadLen / wordLen

	// process payload word by word
	// we use a classic for loop as it's faster than `i := range payload`
	for i := 0; i < numWords; i++ {
		// get word slice
		wordSlice := payload[i*wordLen : i*wordLen+wordLen]

		// make uin32
		word := binary.BigEndian.Uint32(wordSlice)
		// unmask/mask
		word ^= mask32
		// put word back into payload
		binary.BigEndian.PutUint32(wordSlice, word)
	}

	// Handle any remaining bytes (less than 4) at the end
	for i := numWords * wordLen; i < payloadLen; i++ {
		//unmask normally
		payload[i] ^= maskingKey[i%4]
	}
}

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
