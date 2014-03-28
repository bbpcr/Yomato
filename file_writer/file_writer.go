package file_writer

import (
	"github.com/bbpcr/Yomato/torrent_info"
	"os"
	"path/filepath"
)

type PieceData struct {
	PieceNumber int
	Offset      int
	Piece       []byte
}

// Writes torrent pieces to a file, as needed.
type Writer struct {
	Root        string
	TorrentInfo torrent_info.TorrentInfo
}

func New(root string, torrent torrent_info.TorrentInfo) Writer {
	err := os.MkdirAll(root, 0777)
	if err != nil {
		panic(err)
	}

	return Writer{
		Root:        root,
		TorrentInfo: torrent,
	}
}

func (writer Writer) WritePiece(file *os.File, offset int64, piece []byte) {
	file.Seek(offset, 0)
	_, err := file.Write(piece)
	if err != nil {
		panic(err)
	}
	file.Sync()
}

func (writer Writer) StartWriting(comm chan PieceData) {

	var filesArray []*os.File
	var folderPath string = ""

	if writer.TorrentInfo.FileInformations.MultipleFiles {
		err := os.MkdirAll(filepath.Join(writer.Root, writer.TorrentInfo.FileInformations.RootPath), 0777)
		folderPath = writer.TorrentInfo.FileInformations.RootPath
		if err != nil {
			panic(err)
		}
	}

	for _, fileData := range writer.TorrentInfo.FileInformations.Files {
		fullFilepath := filepath.Join(writer.Root, folderPath, fileData.Name)
		file, err := os.OpenFile(fullFilepath, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			panic(err)
		}
		filesArray = append(filesArray, file)

	}

	defer (func() {
		for _, file := range filesArray {
			file.Close()
		}
	})()

	for {
		select {
		case data, ok := <-comm:
			if !ok {
				return
			}
			offset := int64(data.PieceNumber)*writer.TorrentInfo.FileInformations.PieceLength + int64(data.Offset)

			// search the right file and offset
			var currentFileIndex int = 0

			for index, _ := range filesArray {
				if writer.TorrentInfo.FileInformations.Files[index].Length > offset {
					currentFileIndex = index
					break
				} else {
					offset -= writer.TorrentInfo.FileInformations.Files[index].Length
				}
			}

			bytesToWrite := int64(len(data.Piece))

			for ; bytesToWrite > 0; currentFileIndex++ {

				bucketSize := writer.TorrentInfo.FileInformations.Files[currentFileIndex].Length - offset
				if bytesToWrite > bucketSize {

					writer.WritePiece(filesArray[currentFileIndex], offset, data.Piece[:bucketSize])
					bytesToWrite -= bucketSize
					data.Piece = data.Piece[bucketSize:]
					offset = 0

				} else {
					writer.WritePiece(filesArray[currentFileIndex], offset, data.Piece)
					bytesToWrite = 0
				}
			}
		}
	}
}
