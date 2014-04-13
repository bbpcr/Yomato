// Package downloader implements basic functions for downloading a torrent file
package downloader

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/bbpcr/Yomato/bencode"
	"github.com/bbpcr/Yomato/bitfield"
	"github.com/bbpcr/Yomato/file_writer"
	"github.com/bbpcr/Yomato/local_server"
	"github.com/bbpcr/Yomato/peer"
	"github.com/bbpcr/Yomato/torrent_info"
	"github.com/bbpcr/Yomato/tracker"
)

const (
	BLOCK_LENGTH = 1 << 14
)

const (
	NOT_COMPLETED = iota
	DOWNLOADING
	COMPLETED
)

type PieceManager struct {
	BlockBytes map[int]int //tells me how much i need to download from a block [block:bytes]
	BlockOffset map[int]int //tells me the offset of the block in piece [block:pieceOffset]
	BlockDownloading map[int]bool //tells me if a block is downloading [block:true/false]
	BlockPiece map[int]int //tells me what piece the block belongs [block:piece]
	PieceBytes map[int]int //tells me how much i downloaded from a piece [piece:bytes]
	PieceNumBlocks map[int]int //tells me how many blocks a piece has [piece:numBlocks]
}

func (manager PieceManager) getBlockIndex(pieceIndex int , offsetIndex int) int {
	startPosition := pieceIndex * manager.PieceNumBlocks[pieceIndex]
	howMany := offsetIndex / BLOCK_LENGTH
	if (offsetIndex % BLOCK_LENGTH != 0){
		howMany++
	}	
	startPosition += howMany
	return startPosition
}

type Downloader struct {
	Trackers    []tracker.Tracker
	TorrentInfo torrent_info.TorrentInfo
	LocalServer *local_server.LocalServer
	PeerId      string
	GoodPeers   []peer.Peer
	Bitfield    *bitfield.Bitfield
	Status      int
	Downloaded  int64
	Speed       float64
	Manager     PieceManager
}

func (downloader Downloader) RequestPeers(comm chan peer.PeerCommunication, bytesUploaded, bytesDownloaded, bytesLeft int64) {

	// Request the peers , from the tracker
	// The first paramater is how many bytes uploaded , the second downloaded , and the third remaining size
	for trackerIndex := 0; trackerIndex < len(downloader.Trackers); trackerIndex++ {

		data, err := downloader.Trackers[trackerIndex].RequestPeers(bytesUploaded, bytesDownloaded, bytesLeft)

		if err != nil {
			continue
		}

		for peerIndex := 0; peerIndex < len(data); peerIndex++ {
			go data[peerIndex].EstablishFullConnection(comm)
		}
	}
}

// StartDownloading downloads the motherfucker
func (downloader *Downloader) StartDownloading() {

	downloader.Downloaded = 0
	if downloader.Status == DOWNLOADING {
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	writer := file_writer.New(filepath.Join(cwd, "TorrentDownloads"), downloader.TorrentInfo)
	writerChan := make(chan file_writer.PieceData)
	comm := make(chan peer.PeerCommunication)
	go writer.StartWriting(writerChan)

	startedTime := time.Now()
	downloader.RequestPeers(comm, downloader.Downloaded, 0, downloader.TorrentInfo.FileInformations.TotalLength-downloader.Downloaded)

	ticker := time.NewTicker(time.Second * 1)
	go func() {
		var lastDownloaded int64 = 0
		seconds := 0
		for _ = range ticker.C {
			seconds++
			downloader.Speed = float64(downloader.Downloaded-lastDownloaded) / 1024.0
			lastDownloaded = downloader.Downloaded
			fmt.Println(fmt.Sprintf("========= Downloaded Pieces : %d / %d Downloaded : %d KB / %d KB (%.2f%%) Speed : %.2f KB/s Elapsed : %.2f seconds =========", downloader.Bitfield.OneBits, downloader.Bitfield.Length, downloader.Downloaded, downloader.TorrentInfo.FileInformations.TotalLength, 100.0*float64(downloader.Downloaded)/float64(downloader.TorrentInfo.FileInformations.TotalLength), downloader.Speed, time.Since(startedTime).Seconds()))
			if seconds == 10 {
				go downloader.RequestPeers(comm, downloader.Downloaded, 0, downloader.TorrentInfo.FileInformations.TotalLength-downloader.Downloaded)
				seconds = 0
			}
		}
	}()

	
	defer ticker.Stop()
	
	blockIndex := 0
	
	for pieceIndex := 0; pieceIndex < int(downloader.TorrentInfo.FileInformations.PieceCount); pieceIndex++ {
	
		pieceLength := downloader.TorrentInfo.FileInformations.PieceLength
		if pieceIndex == int(downloader.TorrentInfo.FileInformations.PieceCount) - 1 {
			pieceLength = downloader.TorrentInfo.FileInformations.TotalLength - downloader.TorrentInfo.FileInformations.PieceLength*(downloader.TorrentInfo.FileInformations.PieceCount-1)
		}
		numBlocks := pieceLength / BLOCK_LENGTH
		lastBlockSize := pieceLength % BLOCK_LENGTH
		offset := 0
		
		for blockPosition := 0; blockPosition < int(numBlocks); blockPosition++ {
			downloader.Manager.BlockBytes[blockIndex] = BLOCK_LENGTH
			downloader.Manager.BlockDownloading[blockIndex] = false
			downloader.Manager.BlockPiece[blockIndex] = pieceIndex
			downloader.Manager.BlockOffset[blockIndex] = offset
			blockIndex++;
			offset += BLOCK_LENGTH
		}
		
		downloader.Manager.PieceBytes[pieceIndex] = 0
		downloader.Manager.PieceNumBlocks[pieceIndex] = int(numBlocks)
		
		if (lastBlockSize != 0) {
			downloader.Manager.BlockBytes[blockIndex] = int(lastBlockSize)
			downloader.Manager.BlockDownloading[blockIndex] = false
			downloader.Manager.BlockPiece[blockIndex] = pieceIndex
			downloader.Manager.PieceNumBlocks[pieceIndex] ++
			downloader.Manager.BlockOffset[blockIndex] = offset
			blockIndex++
		}	
	}
	
	
	peers := 0
	for downloader.Downloaded < downloader.TorrentInfo.FileInformations.TotalLength {
		select {
		case msg, _ := <-comm:
			receivedPeer := msg.Peer
			msgID := msg.MessageID
			status := msg.StatusMessage
			if msgID == peer.REQUEST && status == "OK" {

				pieceIndex := int(binary.BigEndian.Uint32(msg.BytesReceived[0:4]))
				pieceOffset := int(binary.BigEndian.Uint32(msg.BytesReceived[4:8]))
				pieceBytes := msg.BytesReceived[8:]
				
				blockIndex := downloader.Manager.getBlockIndex(pieceIndex , pieceOffset)
				downloader.Manager.BlockBytes[blockIndex] -= len(pieceBytes)
				downloader.Manager.BlockDownloading[blockIndex] = false
				downloader.Manager.PieceBytes[pieceIndex] += len(pieceBytes)
				downloader.Downloaded += int64(len(pieceBytes))

				writerChan <- file_writer.PieceData{pieceIndex, pieceOffset, pieceBytes}
				downloader.checkPieceCompleted(blockIndex, pieceIndex)
				fiveBlocks := downloader.GetNext5BlocksToDownload(receivedPeer)
				if fiveBlocks != nil {
					go receivedPeer.RequestPiece(comm, downloader.Manager.BlockPiece[fiveBlocks[0]] , downloader.Manager.BlockOffset[fiveBlocks[0]] , downloader.Manager.BlockBytes[fiveBlocks[0]])
					downloader.Manager.BlockDownloading[fiveBlocks[0]] = true
				}

			} else if msgID == peer.REQUEST && status != "OK" {
			
					pieceIndex := int(binary.BigEndian.Uint32(msg.BytesReceived[0:4]))
					pieceOffset := int(binary.BigEndian.Uint32(msg.BytesReceived[4:8]))
					blockIndex := downloader.Manager.getBlockIndex(pieceIndex , pieceOffset)
					downloader.Manager.BlockDownloading[blockIndex] = false
					receivedPeer.Disconnect()
					
			} else if msgID == peer.FULL_CONNECTION && status == "OK" {
			
				peers++
				fiveBlocks := downloader.GetNext5BlocksToDownload(receivedPeer)
				if fiveBlocks != nil {
					go receivedPeer.RequestPiece(comm, downloader.Manager.BlockPiece[fiveBlocks[0]] , downloader.Manager.BlockOffset[fiveBlocks[0]] , downloader.Manager.BlockBytes[fiveBlocks[0]])
					downloader.Manager.BlockDownloading[fiveBlocks[0]] = true
				} else {
				
				}

			} else if msgID == peer.FULL_CONNECTION && status != "OK" {
			}
		}
	}

	downloader.Status = COMPLETED
	return
}

func (downloader Downloader) checkPieceCompleted(blockIndex int, pieceIndex int) {

	if pieceIndex == int(downloader.TorrentInfo.FileInformations.PieceCount-1) {
		// If it's the last piece , we need to treat it better.
		// The last piece has lesser size
		if downloader.TorrentInfo.FileInformations.PieceCount >= 2 {
			lastPieceLength := downloader.TorrentInfo.FileInformations.TotalLength - downloader.TorrentInfo.FileInformations.PieceLength*(downloader.TorrentInfo.FileInformations.PieceCount-1)
			if int64(downloader.Manager.PieceBytes[pieceIndex]) >= lastPieceLength {
				//Finished
				downloader.Bitfield.Set(pieceIndex, true)
			}
		}

	} else if int64(downloader.Manager.PieceBytes[pieceIndex]) >= downloader.TorrentInfo.FileInformations.PieceLength {
		//Finished
		downloader.Bitfield.Set(pieceIndex , true)
	}
}


// New returns a Downloader from a torrent file.
func New(torrent_path string) *Downloader {
	data, err := ioutil.ReadFile(torrent_path)
	if err != nil {
		panic(err)
	}
	res, _, err := bencode.Parse(data)
	if err != nil {
		panic(err)
	}

	var torrentInfo *torrent_info.TorrentInfo
	if torrentInfo, err = torrent_info.GetInfoFromBencoder(res); err != nil {
		panic(err)
	}

	file_bitfield := bitfield.New(int(torrentInfo.FileInformations.PieceCount))
	peerId := createPeerId()
	downloader := &Downloader{
		TorrentInfo: *torrentInfo,
		PeerId:      peerId,
		Bitfield:    &file_bitfield,
	}
	downloader.LocalServer = local_server.New(peerId)
	downloader.Trackers = make([]tracker.Tracker, 1+len(torrentInfo.AnnounceList))

	mainTracker := tracker.New(torrentInfo.AnnounceUrl, torrentInfo, downloader.LocalServer.Port, peerId)
	downloader.Trackers[0] = mainTracker

	for announcerIndex, announcerUrl := range torrentInfo.AnnounceList {
		tracker := tracker.New(announcerUrl, torrentInfo, downloader.LocalServer.Port, peerId)
		downloader.Trackers[announcerIndex+1] = tracker
	}
	downloader.Status = NOT_COMPLETED
	downloader.Manager = PieceManager {
		BlockBytes : make(map[int]int),
		BlockOffset : make(map[int]int),
		BlockDownloading : make(map[int]bool),
		BlockPiece : make(map[int]int),
		PieceBytes : make(map[int]int),
		PieceNumBlocks : make(map[int]int),
	}
	
	return downloader
}

// Returns the ID of the next piece to download.
// This can use multiple strategies, e.g.
// Sequentially (NOT good, easy for development)
// or randomized (much better)
func (downloader *Downloader) GetNext5BlocksToDownload(for_peer *peer.Peer) ([]int) {
	var blocks []int
	count := 0
	for block := 0 ; block < len(downloader.Manager.BlockDownloading) && count < 5; block++ {
		if !downloader.Manager.BlockDownloading[block] && for_peer.BitfieldInfo.At(downloader.Manager.BlockPiece[block]) && downloader.Manager.BlockBytes[block] > 0 {
			blocks = append(blocks , block)
			count ++
		}
	}
	return blocks
}

func createPeerId() string {
	const idSize = 20
	const prefix = "-YM"
	const alphanumerics = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	var data = make([]byte, idSize-len(prefix))
	rand.Read(data)
	for i, b := range data {
		data[i] = alphanumerics[b%byte(len(alphanumerics))]
	}
	return prefix + string(data)
}
