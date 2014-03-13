package main

import (
	"fmt"
	"os"

	"github.com/bbpcr/Yomato/downloader"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: yomato [file.torrent]")
		return
	}

	path := os.Args[1]

	download := downloader.New(path)
	fmt.Println(download.TorrentInfo.Description())
	download.StartDownloading()
}
