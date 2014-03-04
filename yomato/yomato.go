package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/bbpcr/Yomato/bencode"
	"github.com/bbpcr/Yomato/torrent_info"
	"github.com/bbpcr/Yomato/tracker"
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

	tracker := tracker.New(*torrentInfo)
	response, err := tracker.Start()
	if err != nil {
		panic(err)
	}

	fmt.Println(torrentInfo.Description())
	fmt.Printf("Tracker response: %s\n", response.Dump())
}
