package main

import (
	"bencode"
	// "fmt"
	"io/ioutil"
	"os"
)

func main() {
	path := "/home/gabi/Downloads/Windows XP Professional SP3 (x86) Integrated March 2013-Fl.torrent"
	data, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	res, _, err := bencode.Parse(data)
	if err != nil {
		panic(err)
	}

	os.Stdout.Write(res.Encode())
	// fmt.Println(res.Dump())
}
