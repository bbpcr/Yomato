package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/bbcpr/Yomato/bencode"
	"github.com/bbcpr/Yomato/torrent_info"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: yomato [file.torrent]")
		return
	}

	path := os.Args[1]
	data, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	res, _, err := bencode.Parse(data)
	if err != nil {
		panic(err)
	}

	var torrentInfo *torrent_info.TorrentInfo
	if torrentInfo, err = torrent_info.GetInfoFromBencoder(res); err != nil {
		panic(err)
	}

	fmt.Println(torrentInfo.Description())
}
