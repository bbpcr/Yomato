package user_interface

import (
	"fmt"
	"github.com/bbpcr/Yomato/downloader"
	"github.com/mattn/go-gtk/gdkpixbuf"
	"github.com/mattn/go-gtk/glib"
	"github.com/mattn/go-gtk/gtk"
	"strconv"
	"time"
)

type LoadBotton struct {
	button      *gtk.Button
	filechooser *gtk.FileChooserDialog
}

type UserInterface struct {
	MainWindow   *gtk.Window
	VerticalBox  *gtk.VBox
	Load         *LoadBotton
	TorrentFrame *gtk.Frame
	LoadFrame    *gtk.Frame
	TorrentList  *GTKTorrentList
	XDimension   int
	YDimension   int
}
type GTKTorrentList struct {
	store       *gtk.ListStore
	treeview    *gtk.TreeView
	paths       []string
	downloaders []*downloader.Downloader
}

func (ui *UserInterface) CreateWindow() {
	ui.MainWindow = gtk.NewWindow(gtk.WINDOW_TOPLEVEL)
	ui.MainWindow.SetDefaultSize(ui.XDimension, ui.YDimension)
}
func (ui *UserInterface) CreateVBox() {
	ui.VerticalBox = gtk.NewVBox(false, 1)
}
func NewLoadButton(label string) *LoadBotton {
	lb := &LoadBotton{
		button: gtk.NewButtonWithLabel(label),
	}
	return lb
}

func NewUI(X, Y int) *UserInterface {
	ui := &UserInterface{
		XDimension: X,
		YDimension: Y,
	}

	return ui
}
func NewGTKTorrentList() *GTKTorrentList {
	Tlist := &GTKTorrentList{
		store:    gtk.NewListStore(glib.G_TYPE_STRING, glib.G_TYPE_INT, glib.G_TYPE_STRING),
		treeview: gtk.NewTreeView(),
		paths:    []string{},
	}
	Tlist.treeview.SetModel(Tlist.store)
	Tlist.treeview.AppendColumn(
		gtk.NewTreeViewColumnWithAttributes("Name", gtk.NewCellRendererText(), "text", 0))

	Tlist.treeview.AppendColumn(
		gtk.NewTreeViewColumnWithAttributes("Progress", gtk.NewCellRendererProgress(), "value", 1))

	Tlist.treeview.AppendColumn(
		gtk.NewTreeViewColumnWithAttributes("Download Speed", gtk.NewCellRendererText(), "text", 2))
	return Tlist
}

func (bt *LoadBotton) RunFileChooser() {
	bt.filechooser.Run()
}
func (Tlist *GTKTorrentList) AddTorrentDescription(TorrentPath string) {
	var iter gtk.TreeIter
	Tlist.paths = append(Tlist.paths, TorrentPath)

	now_download := downloader.New(TorrentPath)
	Tlist.downloaders = append(Tlist.downloaders, now_download)
	torrent_name := now_download.TorrentInfo.FileInformations.RootPath

	Tlist.store.Append(&iter)
	Tlist.store.Set(&iter, torrent_name, 0, "0.00KB/s")

	//This is how we see the attributes
	/*
		var attr_val glib.GValue
		Tlist.store.GetValue(&iter, 0, &attr_val)
		fmt.Println(attr_val.Value)
	*/

}

func (ui *UserInterface) CreateFileChooser() {
	ui.Load.filechooser = gtk.NewFileChooserDialog(
		"Choose Torrent file...",
		ui.Load.button.GetTopLevelAsWindow(),
		gtk.FILE_CHOOSER_ACTION_OPEN,
		gtk.STOCK_OK,
		gtk.RESPONSE_ACCEPT)

	//Make sure we accept only .torrent files
	torrentfilter := gtk.NewFileFilter()
	torrentfilter.AddPattern("*.torrent")
	torrentfilter.SetName("Torrent Files")
	ui.Load.filechooser.AddFilter(torrentfilter)

	//Mainly adding each time a torrent when load
	//TODO: check if torrent valid
	ui.Load.filechooser.Response(func() {
		torrent_name := ui.Load.filechooser.GetFilename()
		ui.TorrentList.AddTorrentDescription(torrent_name)
		ui.Load.filechooser.Destroy()
		fmt.Println("loading file...over")
	})
}
func XpmLabelBox(path, label_text string) (*gtk.HBox, *glib.Error) {

	box1 := gtk.NewHBox(false, 0)
	box1.SetBorderWidth(2)

	pixbuf, err := gdkpixbuf.NewFromFileAtScale(path, 50, 60, true)
	if err != nil {
		return nil, err
	}
	image := gtk.NewImage()
	image.SetFromPixbuf(pixbuf)

	label := gtk.NewLabel(label_text)
	box1.PackStart(image, false, false, 3)
	box1.PackStart(label, false, false, 3)
	box1.ShowAll()

	return box1, nil

}

func (ui *UserInterface) AddFirstFrame() {

	ui.LoadFrame = gtk.NewFrame("Torrent Loader")
	ui.LoadFrame.SetBorderWidth(5)

	ui.TorrentFrame = gtk.NewFrame("List of Torrents Loaded")
	ui.TorrentFrame.SetBorderWidth(5)

	hbox := gtk.NewHBox(false, 5)
	hbox.SetSizeRequest(400, 50)
	hbox.SetBorderWidth(5)

	ui.Load = NewLoadButton("Load File")

	ui.Load.button.Clicked(func() {
		fmt.Println("button clicked:", ui.Load.button.GetLabel())

		ui.CreateFileChooser()
		ui.Load.RunFileChooser()
	})
	hbox.PackStart(ui.Load.button, true, false, 0)

	//Make the start button
	hbox.PackStart(gtk.NewVSeparator(), false, false, 0)
	start_button := gtk.NewButton()
	start_button.Connect("clicked", func() {

		//make sure we didn't click start before having some files loaded
		if len(ui.TorrentList.paths) == 0 {
			return
		}

		tree_selection := ui.TorrentList.treeview.GetSelection()

		fmt.Println("pushed the start button")

		var iter gtk.TreeIter
		tree_selection.GetSelected(&iter)
		torrent_name_str := ui.TorrentList.store.GetStringFromIter(&iter)
		torrent_index, err := strconv.Atoi(torrent_name_str)

		if err != nil {
			return
		}
		go func() {
			ui.TorrentList.downloaders[torrent_index].StartDownloading()
		}()
		fmt.Println(ui.TorrentList.paths[torrent_index])
	}, "Download me!")

	xpm_box, err := XpmLabelBox("user_interface/play.jpg", "Download me!")
	if err != nil {
		fmt.Print("Cannot load the start image")
		return
	}

	start_button.Add(xpm_box)
	hbox.PackStart(start_button, false, false, 0)

	ui.LoadFrame.Add(hbox)
	ui.VerticalBox.PackStart(ui.LoadFrame, false, true, 0)

}
func (ui *UserInterface) update() {
	if len(ui.TorrentList.paths) == 0 {
		return
	}
	var iter gtk.TreeIter
	ui.TorrentList.store.GetIterFirst(&iter)

	for i := 0; i < len(ui.TorrentList.paths); i++ {

		//there's got to be a way to update smarther than this..
		//shit happens when the loop doesn't end and continues after one torrent is added

		//update the speed

		ui.TorrentList.store.SetValue(&iter, 2, fmt.Sprintf("%.2f", ui.TorrentList.downloaders[i].Speed)+"KB/s")

		var current_torrent = ui.TorrentList.downloaders[i]

		var attr_value = int(current_torrent.Downloaded * 100 / current_torrent.TorrentInfo.FileInformations.TotalLength)

		ui.TorrentList.store.SetValue(&iter, 1, attr_value)
		if i != len(ui.TorrentList.paths)-1 {
			ui.TorrentList.store.IterNext(&iter)
		}
	}
}
func Wrapper() *gtk.Window {

	ui := NewUI(700, 300)
	ui.CreateWindow()
	ui.CreateVBox()

	ui.TorrentList = NewGTKTorrentList()
	// The frame with the Load Button
	ui.AddFirstFrame()
	ui.VerticalBox.Add(ui.TorrentList.treeview)

	ui.MainWindow.Add(ui.VerticalBox)
	ui.MainWindow.ShowAll()
	ticker := time.NewTicker(time.Second * 1)
	go func() {
		var seconds = 0
		for _ = range ticker.C {
			seconds++
			if seconds == 10 {
				ui.update()
				seconds = 0
			}

		}
	}()
	return ui.MainWindow
}
