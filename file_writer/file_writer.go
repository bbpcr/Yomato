package file_writer

import (
	"bytes"
	"crypto/sha1"
	"github.com/bbpcr/Yomato/piece_manager"
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
	mutex       sync.Mutex
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
}

// check whether the SHA-1 sums match for all the pieces in the file
func (writer *Writer) CheckSha1Sums() []int64 {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()

	if len(writer.filesArray) == 0 {
		writer.prepareFilesArray()
	}
	pieceLength := writer.TorrentInfo.FileInformations.PieceLength
	buffer := make([]byte, pieceLength)
	var fileIndex, fileStart, fileEnd int64 = 0, 0, pieceLength
	corruptedPieces := make([]int64, 0)
	for pieceNum := int64(0); pieceNum < writer.TorrentInfo.FileInformations.PieceCount; pieceNum++ {
		for fileStart > writer.TorrentInfo.FileInformations.Files[fileIndex].Length {
			fileStart -= writer.TorrentInfo.FileInformations.Files[fileIndex].Length
			fileIndex++
		}

		if pieceNum == writer.TorrentInfo.FileInformations.PieceCount-1 {
			fileEnd = fileStart + writer.TorrentInfo.FileInformations.TotalLength - writer.TorrentInfo.FileInformations.PieceLength*(writer.TorrentInfo.FileInformations.PieceCount-1)
		} else {
			fileEnd = fileStart + pieceLength
		}

		bufferOffset := int64(0)
		for bufferOffset < int64(len(buffer)) && fileIndex < int64(len(writer.filesArray)) {
			file := writer.filesArray[fileIndex]
			fileLength := writer.TorrentInfo.FileInformations.Files[fileIndex].Length
			file.Seek(fileStart, 0)

			if fileEnd < fileLength {
				file.Read(buffer[bufferOffset:len(buffer)])
				bufferOffset = int64(len(buffer))
			} else {

				file.Read(buffer[bufferOffset : bufferOffset+fileLength-fileStart])
				bufferOffset = bufferOffset + fileLength - fileStart
				fileEnd -= fileLength
				fileStart = 0
				fileIndex++
			}
		}

		hash := writer.TorrentInfo.FileInformations.Pieces[pieceNum*20 : (pieceNum+1)*20]
		computedHash := sha1.New()
		computedHash.Write(buffer[0:bufferOffset])

		if !bytes.Equal(hash, computedHash.Sum(nil)) {
			corruptedPieces = append(corruptedPieces, pieceNum)
		}
		fileStart = fileEnd
	}

	return corruptedPieces
}

func (writer *Writer) prepareFilesArray() {
	folderPath := ""
	writer.filesArray = make([]*os.File, 0)

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
}

func (writer *Writer) StartWriting(comm chan PieceData, done chan PieceData) {
	writer.prepareFilesArray()

	defer (func() {
		for _, file := range writer.filesArray {
			file.Close()
		}
	})()

	buffers := make(map[int64][]byte)
	bufferBlocks := make(map[int64][]int64)

	for {
		select {
		case data, ok := <-comm:
			if !ok {
				return
			}

			blockIndex := int64(data.Offset / piece_manager.BLOCK_LENGTH)
			pieceNum := int64(data.PieceNumber)

			if _, ok := bufferBlocks[pieceNum]; !ok {
				bufferBlocks[pieceNum] = make([]int64, 0)
				buffers[pieceNum] = make([]byte, writer.TorrentInfo.FileInformations.PieceLength, writer.TorrentInfo.FileInformations.PieceLength)
			}

			bufferBlocks[pieceNum] = append(bufferBlocks[pieceNum], blockIndex)
			copy(buffers[pieceNum][data.Offset:data.Offset+len(data.Piece)], data.Piece)

			numBlocks := writer.TorrentInfo.FileInformations.PieceLength / piece_manager.BLOCK_LENGTH
			pieceSize := writer.TorrentInfo.FileInformations.PieceLength
			if int64(pieceNum+1) == writer.TorrentInfo.FileInformations.PieceCount {
				pieceSize = writer.TorrentInfo.FileInformations.TotalLength - writer.TorrentInfo.FileInformations.PieceLength*(writer.TorrentInfo.FileInformations.PieceCount-1)
				numBlocks = pieceSize / piece_manager.BLOCK_LENGTH
				if pieceSize%piece_manager.BLOCK_LENGTH != 0 {
					numBlocks++
				}
			}

			if int64(len(bufferBlocks[pieceNum])) == numBlocks {
				// write it
				offset := int64(pieceNum) * writer.TorrentInfo.FileInformations.PieceLength

				totalData := 0
				for _ = range buffers {
					totalData += int(writer.TorrentInfo.FileInformations.PieceLength)
				}

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

				bytesToWrite := int64(pieceSize)

				dataToWrite := buffers[pieceNum]
				writer.mutex.Lock()

				for ; bytesToWrite > 0; currentFileIndex++ {

					bucketSize := writer.TorrentInfo.FileInformations.Files[currentFileIndex].Length - offset
					if bytesToWrite > bucketSize {

						writer.WritePiece(writer.filesArray[currentFileIndex], offset, dataToWrite[:bucketSize])
						bytesToWrite -= bucketSize
						dataToWrite = dataToWrite[bucketSize:]
						offset = 0

					} else {
						writer.WritePiece(writer.filesArray[currentFileIndex], offset, dataToWrite[:bytesToWrite])
						bytesToWrite = 0
					}
				}
				writer.mutex.Unlock()

				for _, blockDoneIndex := range bufferBlocks[pieceNum] {
					data.Offset = int(blockDoneIndex * piece_manager.BLOCK_LENGTH)
					done <- data
				}

				delete(buffers, pieceNum)
				delete(bufferBlocks, pieceNum)

			}
		}
	}
}
