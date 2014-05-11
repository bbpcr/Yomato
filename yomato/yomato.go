package main

import (
	"fmt"
	"github.com/bbpcr/Yomato/user_interface"
	"github.com/mattn/go-gtk/glib"
	"github.com/mattn/go-gtk/gtk"
)

func main() {
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
