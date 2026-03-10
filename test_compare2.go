package main

import (
	"crypto/subtle"
	"fmt"
)

func main() {
	reqCode := []byte("hello")
	siteCode := []byte("hello")
	res := subtle.ConstantTimeCompare(reqCode, siteCode)
	fmt.Println(res)
}
