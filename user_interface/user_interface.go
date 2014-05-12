package user_interface

import (
	"fmt"
	"github.com/bbpcr/Yomato/downloader"
	"github.com/mattn/go-gtk/glib"
	"github.com/mattn/go-gtk/gtk"
	"strconv"
	"strings"
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
	store         *gtk.ListStore
	treeview      *gtk.TreeView
	paths         []string
	downloaders   []*downloader.Downloader
	started_paths []bool
}

func (ui *UserInterface) CreateWindow() {
	ui.MainWindow = gtk.NewWindow(gtk.WINDOW_TOPLEVEL)
	ui.MainWindow.SetDefaultSize(ui.XDimension, ui.YDimension)
}
func (ui *UserInterface) CreateVBox() {
	ui.VerticalBox = gtk.NewVBox(false, 1)
}
func DefaultImageBox(StockItem string) *gtk.HBox {

	box1 := gtk.NewHBox(false, 0)
	box1.SetBorderWidth(2)

	image := gtk.NewImage()
	image.SetFromPixbuf(gtk.NewImage().RenderIcon(StockItem, gtk.ICON_SIZE_SMALL_TOOLBAR, ""))

	box1.PackStart(image, false, false, 3)
	box1.ShowAll()

	return box1

}
func NewLoadButton() *LoadBotton {
	lb := &LoadBotton{
		button: gtk.NewButton(),
	}
	lb.button.Add(DefaultImageBox(gtk.STOCK_ADD))
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

	//Not adding duplicates

	now_download := downloader.New(TorrentPath)

	for i := 0; i < len(Tlist.downloaders); i++ {
		if Tlist.downloaders[i].TorrentInfo.FileInformations.RootPath == now_download.TorrentInfo.FileInformations.RootPath {
			return
		}
	}
	Tlist.paths = append(Tlist.paths, TorrentPath)
	Tlist.started_paths = append(Tlist.started_paths, false)
	Tlist.downloaders = append(Tlist.downloaders, now_download)
	torrent_name := now_download.TorrentInfo.FileInformations.RootPath

	Tlist.store.Append(&iter)
	Tlist.store.Set(&iter, torrent_name, 0, "Not Started")

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

	ui.Load.filechooser.Response(func() {
		torrent_path := ui.Load.filechooser.GetFilename()
		splitted_path := strings.Split(torrent_path, ".")
		is_torrent := (splitted_path[len(splitted_path)-1] == "torrent")
		if is_torrent == true {

			ui.TorrentList.AddTorrentDescription(torrent_path)
			ui.Load.filechooser.Destroy()

			fmt.Println("loading file...over")
		}
	})
	ui.Load.RunFileChooser()
}

func (ui *UserInterface) AddFirstFrame() {

	ui.LoadFrame = gtk.NewFrame("Torrent Loader")
	ui.LoadFrame.SetBorderWidth(5)

	ui.TorrentFrame = gtk.NewFrame("List of Torrents Loaded")
	ui.TorrentFrame.SetBorderWidth(5)

	hbox := gtk.NewHBox(false, 5)
	hbox.SetSizeRequest(400, 50)
	hbox.SetBorderWidth(5)

	ui.Load = NewLoadButton()

	ui.Load.button.Clicked(func() {

		ui.CreateFileChooser()

	})
	hbox.PackStart(ui.Load.button, false, false, 0)

	//Make the start button
	start_button := gtk.NewButton()
	start_button.Connect("clicked", func() {

		//make sure we didn't click start before having some files loaded
		tree_selection := ui.TorrentList.treeview.GetSelection()

		if tree_selection.CountSelectedRows() == 0 {
			return
		}

		var iter gtk.TreeIter
		tree_selection.GetSelected(&iter)

		torrent_name_str := ui.TorrentList.store.GetStringFromIter(&iter)
		torrent_index, err := strconv.Atoi(torrent_name_str)

		if ui.TorrentList.started_paths[torrent_index] == true {
			return
		}
		if err != nil {
			return
		}
		go func() {
			ui.TorrentList.started_paths[torrent_index] = true
			ui.TorrentList.downloaders[torrent_index].StartDownloading()
		}()

	}, "")

	start_button.Add(DefaultImageBox(gtk.STOCK_GOTO_BOTTOM))
	hbox.PackStart(start_button, false, false, 0)

	stop_button := gtk.NewButton()
	stop_button.Add(DefaultImageBox(gtk.STOCK_STOP))

	stop_button.Clicked(func() {
		if len(ui.TorrentList.paths) == 0 {
			return
		}
		tree_selection := ui.TorrentList.treeview.GetSelection()

		var iter gtk.TreeIter
		tree_selection.GetSelected(&iter)

	})
	hbox.PackStart(stop_button, false, false, 0)
	ui.LoadFrame.Add(hbox)
	ui.VerticalBox.PackStart(ui.LoadFrame, false, true, 0)

}
func (ui *UserInterface) update() {
	if len(ui.TorrentList.paths) == 0 {
		return
	}
	var iter gtk.TreeIter
	ui.TorrentList.store.GetIterFirst(&iter)

	number_of_active := len(ui.TorrentList.paths)
	for i := 0; i < number_of_active; i++ {

		//there's got to be a way to update smarther than this..
		//shit happens when the loop doesn't end and continues after one torrent is added

		//update the speed
		if ui.TorrentList.started_paths[i] == false {
			continue
		}
		ui.TorrentList.store.SetValue(&iter, 2, fmt.Sprintf("%.2f", ui.TorrentList.downloaders[i].Speed)+"KB/s")
		var current_torrent = ui.TorrentList.downloaders[i]

		var attr_value = int(current_torrent.Downloaded * 100 / current_torrent.TorrentInfo.FileInformations.TotalLength)

		ui.TorrentList.store.SetValue(&iter, 1, attr_value)
		if i != number_of_active-1 {
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
	swin := gtk.NewScrolledWindow(nil, nil)
	swin.Add(ui.TorrentList.treeview)
	ui.VerticalBox.Add(swin)

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
