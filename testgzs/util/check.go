package util

import "log"

func Check(e error) {
	if e != nil {
		log.Printf("Error: %v", e)
	}
}
