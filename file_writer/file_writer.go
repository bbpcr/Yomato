package file_writer

import (
	"os"

	"github.com/bbpcr/Yomato/torrent_info"
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
	err := os.MkdirAll(root, 0644)
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
	files := make(map[*os.File]int64)
	for _, filedata := range writer.TorrentInfo.FileInformations.Files {
		filepath := writer.Root + writer.TorrentInfo.FileInformations.Files[0].Name
		file, err := os.OpenFile(filepath, os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			panic(err)
		}
		files[file] = filedata.Length
	}
	defer (func() {
		for file, _ := range files {
			file.Close()
		}
	})()

	for {
		select {
		case data, ok := <-comm:
			if !ok {
				return
			}
			offset := int64(data.PieceNumber) * writer.TorrentInfo.FileInformations.PieceLength

			// search the right file and offset
			for file, size := range files {
				if size > offset {
					if int64(len(data.Piece)) > size-offset {
						writer.WritePiece(file, offset, data.Piece[:size-offset])
						data.Piece = data.Piece[:size-offset]
						offset = 0
					} else {
						writer.WritePiece(file, offset, data.Piece)
						break
					}
				}
				offset -= size
			}
		}
	}
}
