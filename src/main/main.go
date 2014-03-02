package main

import (
	"bencode"
	"fmt"
	"io/ioutil"
	"os"
)

func main() {
	path := os.Args[1]
	data, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	res, _, err := bencode.Parse(data)
	if err != nil {
		panic(err)
	}

	// os.Stdout.Write(res.Encode())
	fmt.Println(res.Dump())
}
