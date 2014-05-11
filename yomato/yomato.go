package main

import (
	/*
		"fmt"
		"os"

		"github.com/bbpcr/Yomato/downloader"
	*/
	"fmt"
	"github.com/bbpcr/Yomato/user_interface"
	"github.com/mattn/go-gtk/glib"
	"github.com/mattn/go-gtk/gtk"
)

func main() {
	/*
		if len(os.Args) < 2 {
			fmt.Println("Usage: yomato [file.torrent]")
			return
		}

		path := os.Args[1]

		download := downloader.New(path)
		fmt.Println(download.TorrentInfo.Description())
		download.StartDownloading()
	*/
	gtk.Init(nil)
	window := user_interface.Wrapper()
	window.SetPosition(gtk.WIN_POS_CENTER)
	window.Connect("destroy", func(ctx *glib.CallbackContext) {
		fmt.Println("destroy pending...")
		gtk.MainQuit()
	}, "foo")
	window.ShowAll()
	gtk.Main()

}
