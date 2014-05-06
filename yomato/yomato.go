package main

import (
	"fmt"
	"os"

	"github.com/bbpcr/Yomato/cli"
	"github.com/bbpcr/Yomato/downloader"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: yomato [file.torrent]")
		return
	}

	path, _ := cli.Parse()

	download := downloader.New(path)
	fmt.Println(download.TorrentInfo.Description())
	download.StartDownloading()
}
