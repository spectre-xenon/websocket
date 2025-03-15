package websocket

import "strings"

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
