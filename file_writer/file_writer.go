package file_writer

import (
	"bytes"
	"crypto/sha1"
	"github.com/bbpcr/Yomato/torrent_info"
	"os"
	"path/filepath"
	"sync"
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
	filesArray  []*os.File
	fLocker     sync.Mutex
}

func New(root string, torrent torrent_info.TorrentInfo) *Writer {
	err := os.MkdirAll(root, 0777)
	if err != nil {
		panic(err)
	}

	writer := &Writer{
		Root:        root,
		TorrentInfo: torrent,
	}
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
		err := os.MkdirAll(filepath.Dir(fullFilepath), 0777)
		if err != nil {
			panic(err)
		}
		file, err := os.OpenFile(fullFilepath, os.O_RDWR|os.O_CREATE, 0666)
		if err != nil {
			panic(err)
		}
		writer.filesArray = append(writer.filesArray, file)
	}
	return writer
}

func (writer *Writer) CheckSha1Sum(pieceIndex int64) bool {
	pieceLength := writer.TorrentInfo.FileInformations.PieceLength
	buffer := make([]byte, pieceLength)

	offset := pieceIndex * pieceLength
	// search the right file and offset
	var currentFileIndex int = 0
	for index, _ := range writer.filesArray {
		if writer.TorrentInfo.FileInformations.Files[index].Length > offset {
			currentFileIndex = index
			break
		} else {
			offset -= writer.TorrentInfo.FileInformations.Files[index].Length
		}
	}
	bytesToRead := pieceLength
	bufferPos := int64(0)

	for bytesToRead > 0 {
		writer.fLocker.Lock()
		n, err := writer.filesArray[currentFileIndex].ReadAt(buffer[bufferPos:], offset)
		writer.fLocker.Unlock()
		readed := int64(n)
		if err == nil {
			bufferPos += readed
			bytesToRead -= readed
			offset += readed
		}
		if offset >= writer.TorrentInfo.FileInformations.Files[currentFileIndex].Length {
			currentFileIndex++
			offset = 0
		}
	}

	computedHash := sha1.New()
	computedHash.Write(buffer)
	hash := writer.TorrentInfo.FileInformations.Pieces[pieceIndex*20 : (pieceIndex+1)*20]
	return bytes.Equal(hash, computedHash.Sum(nil))
}

func (writer *Writer) CloseFiles() {
	for _, file := range writer.filesArray {
		file.Close()
	}
}

func (writer *Writer) StartWriting(comm chan PieceData) {

	for {
		select {
		case data, ok := <-comm:
			if !ok {
				return
			}
			offset := int64(data.PieceNumber)*writer.TorrentInfo.FileInformations.PieceLength + int64(data.Offset)

			// search the right file and offset
			var currentFileIndex int = 0

			for index, _ := range writer.filesArray {
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

					writer.fLocker.Lock()
					writer.filesArray[currentFileIndex].WriteAt(data.Piece[:bucketSize], offset)
					writer.fLocker.Unlock()
					bytesToWrite -= bucketSize
					data.Piece = data.Piece[bucketSize:]
					offset = 0

				} else {
					writer.fLocker.Lock()
					writer.filesArray[currentFileIndex].WriteAt(data.Piece, offset)
					writer.fLocker.Unlock()
					bytesToWrite = 0
				}
			}
		}
	}
}
