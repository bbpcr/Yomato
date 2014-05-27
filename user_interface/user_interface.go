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

const (
	SIZE_GIGABYTE = 1024 * 1024 * 1024
	SIZE_MEGABYTE = 1024 * 1024
	SIZE_KILOBYTE = 1024
	SIZE_BYTE     = 1
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
	store        *gtk.ListStore
	treeview     *gtk.TreeView
	paths        []string
	downloaders  []*downloader.Downloader
	active_count int
}

//this is used to show torrents after pressing the load button
type LoadTorrentList struct {
	store           *gtk.TreeStore
	treeview        *gtk.TreeView
	path            string
	fetched_torrent *downloader.Downloader
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

func NewLoadTorrentList(_path string) *LoadTorrentList {

	//name, size, download?
	TList := &LoadTorrentList{
		store:           gtk.NewTreeStore(glib.G_TYPE_STRING, glib.G_TYPE_STRING, glib.G_TYPE_BOOL),
		treeview:        gtk.NewTreeView(),
		path:            _path,
		fetched_torrent: downloader.New(_path),
	}
	TList.treeview.SetModel(TList.store)

	text_box := gtk.NewTreeViewColumnWithAttributes("Name", gtk.NewCellRendererText(), "text", 0)
	text_box.SetResizable(true)
	text_box.SetSizing(gtk.TREE_VIEW_COLUMN_FIXED)
	text_box.SetMinWidth(150)

	TList.treeview.AppendColumn(text_box)

	TList.treeview.AppendColumn(
		gtk.NewTreeViewColumnWithAttributes("Size", gtk.NewCellRendererText(), "text", 1))

	TList.treeview.AppendColumn(
		gtk.NewTreeViewColumnWithAttributes("Download", gtk.NewCellRendererToggle(), "active", 2))

	return TList
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
		store:        gtk.NewListStore(glib.G_TYPE_STRING, glib.G_TYPE_INT, glib.G_TYPE_STRING),
		treeview:     gtk.NewTreeView(),
		paths:        []string{},
		active_count: 0,
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
func (Tlist *GTKTorrentList) GetActiveCount() int {
	count := 0

	for i := 0; i < len(Tlist.downloaders); i++ {
		if Tlist.downloaders[i].Status == downloader.DOWNLOADING {
			count += 1
		}
	}
	return count
}

func (Tlist *GTKTorrentList) DeleteTorrent(torrent_index int, iter gtk.TreeIter) {
	if Tlist.downloaders[torrent_index].Status == downloader.DOWNLOADING {
		fmt.Println("Cannot remove torrent if it's currently downloading")
		return
	}
	Tlist.store.Remove(&iter)
	fmt.Println("Deleted " + Tlist.paths[torrent_index])
	Tlist.paths = append(Tlist.paths[:torrent_index], Tlist.paths[torrent_index+1:]...)
	// Now we should implement some stop routine for downloader
	Tlist.downloaders = append(Tlist.downloaders[:torrent_index], Tlist.downloaders[torrent_index+1:]...)

}
func get_size_in_string(size_checked int64) string {

	var column_size_atr string

	size_fetched := float64(size_checked)

	if size_fetched >= SIZE_GIGABYTE {
		column_size_atr = fmt.Sprintf("%.2f", size_fetched/SIZE_GIGABYTE) + "GB"
	} else if size_fetched >= SIZE_MEGABYTE {
		column_size_atr = fmt.Sprintf("%.2f", size_fetched/SIZE_MEGABYTE) + "MB"
	} else if size_fetched >= SIZE_KILOBYTE {
		column_size_atr = fmt.Sprintf("%.2f", size_fetched/SIZE_KILOBYTE) + "KB"
	} else {
		column_size_atr = fmt.Sprintf("%.2f", size_fetched/SIZE_BYTE) + "Bytes"
	}
	return column_size_atr
}
func (ui *UserInterface) StartLoadingWindow(torrent_path string) {

	//this is where we show the torrent structure

	LoadingWindow := gtk.NewWindow(gtk.WINDOW_TOPLEVEL)
	LoadingWindow.SetPosition(gtk.WIN_POS_CENTER_ON_PARENT)
	LoadingWindow.SetTransientFor(ui.MainWindow)
	LoadingWindow.SetSizeRequest(300, 450)

	vbox := gtk.NewVBox(false, 0)

	//the hbox which contains the ok and cancel buttons

	hbox := gtk.NewHBox(false, 10)

	ok_button := gtk.NewButtonWithLabel("OK")
	ok_button.Clicked(func() {
		ui.TorrentList.AddTorrentDescription(torrent_path)
		LoadingWindow.Destroy()
	})

	cancel_button := gtk.NewButtonWithLabel("Cancel")
	cancel_button.Clicked(func() {
		LoadingWindow.Destroy()
	})

	hbox.PackEnd(cancel_button, false, false, 0)
	hbox.PackEnd(ok_button, false, false, 0)

	LoadList := NewLoadTorrentList(torrent_path)
	fetched_torrent := LoadList.fetched_torrent

	//now adding where to save the file

	path_label := gtk.NewLabel(fetched_torrent.GetDownloadPath())

	button_save := gtk.NewButtonWithLabel("Save to...")
	button_save.Clicked(func() {
		file_saver := gtk.NewFileChooserDialog("Choose where to save",
			LoadingWindow.GetTopLevelAsWindow(),
			gtk.FILE_CHOOSER_ACTION_SELECT_FOLDER,
			gtk.STOCK_OK,
			gtk.RESPONSE_OK)

		file_saver.Run()

		file_saver.Response(func() {
			where_to_download := file_saver.GetFilename()
			fetched_torrent.SetDownloadPath(where_to_download)
			path_label.SetText(where_to_download)
			file_saver.Destroy()
		})
	})

	hbox.PackStart(button_save, false, false, 0)
	vbox.PackEnd(hbox, false, false, 0)
	vbox.PackEnd(path_label, false, false, 0)

	var iter gtk.TreeIter
	LoadList.store.Append(&iter, nil)

	LoadList.store.Set(&iter,
		fetched_torrent.TorrentInfo.FileInformations.RootPath,
		get_size_in_string(fetched_torrent.TorrentInfo.FileInformations.TotalLength),
		true,
	)

	// Main idea: every filename maps to a node in a tree
	// If we reached a filename who doesn't have a node in a tree then tie it to it's ancestor

	var tree map[string]*gtk.TreeIter
	var size_tree map[*gtk.TreeIter]int64

	tree = make(map[string]*gtk.TreeIter)
	size_tree = make(map[*gtk.TreeIter]int64)

	tree["/"] = &iter
	size_tree[&iter] = 0

	for _, file_info := range fetched_torrent.TorrentInfo.FileInformations.Files {

		path := strings.Split(file_info.Name, "/")
		prev_path := ""

		for i := 0; i < len(path); i++ {

			file := path[i]
			current_path := prev_path + "/" + file

			if _, exists := tree[current_path]; exists == false {
				//node in the tree not builed
				iter_prev := tree[prev_path]
				var cur_iter gtk.TreeIter

				LoadList.store.Append(&cur_iter,
					iter_prev,
				)
				LoadList.store.Set(&cur_iter,
					file,
					get_size_in_string(file_info.Length),
					true,
				)
				tree[current_path] = &cur_iter
				size_tree[&cur_iter] = file_info.Length

			} else {
				//node built, just update the size
				size_tree[tree[current_path]] += file_info.Length
			}
			prev_path = prev_path + "/" + file
		}
	}
	for key_path := range tree {
		LoadList.store.SetValue(tree[key_path], 1, get_size_in_string(size_tree[tree[key_path]]))
	}
	swin := gtk.NewScrolledWindow(nil, nil)
	swin.Add(LoadList.treeview)
	swin.ShowAll()
	vbox.Add(swin)
	LoadingWindow.Add(vbox)
	LoadingWindow.ShowAll()

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

			ui.Load.filechooser.Destroy()
			ui.StartLoadingWindow(torrent_path)
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

	// gtk-main-start_button
	start_button := gtk.NewButton()
	start_button.Connect("clicked", func() {

		//make sure we didn't click start before having some files loaded

		tree_selection := ui.TorrentList.treeview.GetSelection()
		if tree_selection.CountSelectedRows() == 0 {
			return
		}

		var iter gtk.TreeIter
		tree_selection.GetSelected(&iter)

		//get the torrent details from the store list
		torrent_name_str := ui.TorrentList.store.GetStringFromIter(&iter)
		torrent_index, err := strconv.Atoi(torrent_name_str)

		if ui.TorrentList.downloaders[torrent_index].Status == downloader.DOWNLOADING {
			return
		}
		downloading := ui.TorrentList.GetActiveCount()
		// if downloading > 0 {
		// 	fmt.Println("Right now we are not currently support multiple downloads")
		// 	return
		// }
		if err != nil {
			return
		}
		fmt.Println(downloading)
		go func() {
			ui.TorrentList.downloaders[torrent_index].StartDownloading()
		}()

	}, "")

	start_button.Add(DefaultImageBox(gtk.STOCK_GOTO_BOTTOM))
	hbox.PackStart(start_button, false, false, 0)
	// end start_button

	// gtk-main-stop_button
	stop_button := gtk.NewButton()
	stop_button.Add(DefaultImageBox(gtk.STOCK_STOP))

	stop_button.Clicked(func() {

		tree_selection := ui.TorrentList.treeview.GetSelection()
		if tree_selection.CountSelectedRows() == 0 {
			return
		}
		var iter gtk.TreeIter
		tree_selection.GetSelected(&iter)
		torrent_name_str := ui.TorrentList.store.GetStringFromIter(&iter)
		torrent_index, err := strconv.Atoi(torrent_name_str)

		if err != nil {
			return
		}
		ui.TorrentList.DeleteTorrent(torrent_index, iter)

	})
	hbox.PackStart(stop_button, false, false, 0)
	// end stop_button
	ui.LoadFrame.Add(hbox)
	ui.VerticalBox.PackStart(ui.LoadFrame, false, true, 0)

}
func (ui *UserInterface) Update() {
	if len(ui.TorrentList.paths) == 0 {
		return
	}
	var iter gtk.TreeIter
	ui.TorrentList.store.GetIterFirst(&iter)
	//TO DO: improvement if we select only the non finished torrents

	number_of_active := len(ui.TorrentList.paths)
	for i := 0; i < number_of_active; i++ {
		//there's got to be a way to update smarther than this..
		//shit happens when the loop doesn't end and continues after one torrent is added
		//not anymore

		//update the speed only if the file started

		//fmt.Println(ui.TorrentList.downloaders[i].Status == downloader.DOWNLOADING)

		if ui.TorrentList.downloaders[i].Status == downloader.DOWNLOADING || ui.TorrentList.downloaders[i].Status == downloader.COMPLETED {

			var current_torrent = ui.TorrentList.downloaders[i]
			ui.TorrentList.store.SetValue(&iter, 2, fmt.Sprintf("%.2f", current_torrent.Speed)+"KB/s")
			//update the progress bar

			var attr_value = int(current_torrent.Bitfield.OneBits * 100 / current_torrent.Bitfield.Length)

			ui.TorrentList.store.SetValue(&iter, 1, attr_value)
		}
		if i != number_of_active-1 {
			ui.TorrentList.store.IterNext(&iter)
		}
	}
}
func Wrapper() *gtk.Window {

	ui := NewUI(500, 600)
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
			if seconds == 3 {
				ui.Update()
				seconds = 0
			}

		}
	}()
	return ui.MainWindow
}
