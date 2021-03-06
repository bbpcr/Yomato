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
		file.Truncate(fileData.Length)
		if err != nil {
			panic(err)
		}
		writer.filesArray = append(writer.filesArray, file)
	}
	return writer
}

func (writer *Writer) CheckSha1Sum(pieceIndex int64) bool {

	pieceLength := writer.TorrentInfo.FileInformations.PieceLength
	if pieceIndex == writer.TorrentInfo.FileInformations.PieceCount-1 {
		pieceLength = writer.TorrentInfo.FileInformations.TotalLength % writer.TorrentInfo.FileInformations.PieceLength
		if pieceLength == 0 {
			pieceLength = writer.TorrentInfo.FileInformations.PieceLength
		}
	}

	offset := pieceIndex * writer.TorrentInfo.FileInformations.PieceLength
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

	buffer := make([]byte, 32*1024)
	computedHash := sha1.New()

	writer.filesArray[currentFileIndex].Seek(offset, 0)

	for bytesToRead > 0 {
		writer.fLocker.Lock()
		var n int
		if bytesToRead < 32*1024 {
			n, _ = writer.filesArray[currentFileIndex].Read(buffer[:bytesToRead])
		} else {
			n, _ = writer.filesArray[currentFileIndex].Read(buffer)
		}
		writer.fLocker.Unlock()
		readed := int64(n)
		if readed != 0 {
			bytesToRead -= readed
			offset += readed
		} else {
			break
		}
		computedHash.Write(buffer[:readed])
		if offset >= writer.TorrentInfo.FileInformations.Files[currentFileIndex].Length {
			currentFileIndex++
			offset = 0
			if currentFileIndex < len(writer.filesArray) {
				writer.filesArray[currentFileIndex].Seek(offset, 0)
			}
		}
	}

	hash := writer.TorrentInfo.FileInformations.Pieces[pieceIndex*20 : (pieceIndex+1)*20]
	return bytes.Equal(hash, computedHash.Sum(nil))
}

func (writer *Writer) CloseFiles() {
	writer.fLocker.Lock()
	defer writer.fLocker.Unlock()
	for _, file := range writer.filesArray {
		file.Close()
	}
}

func (writer *Writer) WritePiece(data PieceData) {
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
