package main

import (
	"bencode"
	"fmt"
	"io/ioutil"
	"os"
	"torrent_info"
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
	fmt.Println("-----")
	
	torrentInfo := torrent_info.GetInfoFromBencoder(res)
	
	fmt.Println(torrentInfo)
	//res.Dump()
}
