package websocket

import (
	"crypto/rand"
	"encoding/binary"
)

// TODO: add docs
func makeMaskingKey() []byte {
	maskingKey := make([]byte, 4)
	// Never returns an error
	_, _ = rand.Read(maskingKey)
	return maskingKey
}

// TODO: add docs
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
