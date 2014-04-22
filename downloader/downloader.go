// Package downloader implements basic functions for downloading a torrent file
package downloader

import (
	"crypto/rand"
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
	"github.com/bbpcr/Yomato/piece_manager"
	"github.com/bbpcr/Yomato/torrent_info"
	"github.com/bbpcr/Yomato/tracker"
)

const (
	NOT_COMPLETED = iota
	DOWNLOADING
	COMPLETED
)

type Downloader struct {
	Trackers       []tracker.Tracker
	TorrentInfo    torrent_info.TorrentInfo
	LocalServer    *local_server.LocalServer
	PeerId         string
	GoodPeers      []peer.Peer
	Bitfield       *bitfield.Bitfield
	Status         int
	Downloaded     int64
	Speed          float64
	Manager        piece_manager.PieceManager
	writerChan     chan file_writer.PieceData
	connectionChan chan peer.ConnectionCommunication
	requestChan    chan peer.RequestCommunication
}

func (downloader Downloader) requestPeers(bytesUploaded, bytesDownloaded, bytesLeft int64) {

	// Request the peers , from the tracker
	// The first paramater is how many bytes uploaded , the second downloaded , and the third remaining size
	for trackerIndex := 0; trackerIndex < len(downloader.Trackers); trackerIndex++ {

		trackerResponse := downloader.Trackers[trackerIndex].RequestPeers(bytesUploaded, bytesDownloaded, bytesLeft)

		for peerIndex := 0; peerIndex < len(trackerResponse.Peers); peerIndex++ {
			go trackerResponse.Peers[peerIndex].EstablishFullConnection(downloader.connectionChan)
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
	go writer.StartWriting(downloader.writerChan)

	startedTime := time.Now()
	downloader.requestPeers(downloader.Downloaded, 0, downloader.TorrentInfo.FileInformations.TotalLength-downloader.Downloaded)

	peers := 0
	ticker := time.NewTicker(time.Second * 1)
	go func() {
		var lastDownloaded int64 = 0
		seconds := 0
		for _ = range ticker.C {
			seconds++
			downloader.Speed = float64(downloader.Downloaded-lastDownloaded) / 1024.0
			lastDownloaded = downloader.Downloaded
			fmt.Println(fmt.Sprintf("=========Peers : %d Downloaded Pieces : %d / %d Downloaded : %d KB / %d KB (%.2f%%) Speed : %.2f KB/s Elapsed : %.2f seconds =========", peers, downloader.Bitfield.OneBits, downloader.Bitfield.Length, downloader.Downloaded, downloader.TorrentInfo.FileInformations.TotalLength, 100.0*float64(downloader.Downloaded)/float64(downloader.TorrentInfo.FileInformations.TotalLength), downloader.Speed, time.Since(startedTime).Seconds()))
			if seconds == 10 {
				go downloader.requestPeers(downloader.Downloaded, 0, downloader.TorrentInfo.FileInformations.TotalLength-downloader.Downloaded)
				seconds = 0
			}
		}
	}()

	defer ticker.Stop()

	for downloader.Downloaded < downloader.TorrentInfo.FileInformations.TotalLength {
		select {

		case connectionMessage, _ := <-downloader.connectionChan:
			if connectionMessage.StatusMessage == "OK" {
				peers++
				downloader.startRequesting(connectionMessage.Peer)
			}

		case piecesMessage, _ := <-downloader.requestChan:

			// On this channel , we receive the data.
			// We also receive empty pieces just flag them as not downloading.

			for _, pieceData := range piecesMessage.Pieces {
				blockIndex := downloader.Manager.GetBlockIndex(pieceData.PieceNumber, pieceData.Offset)
				pieceLength := len(pieceData.Piece)
				if downloader.Manager.BlockBytes[blockIndex] > 0 && pieceLength > 0 {
					downloader.Manager.BlockBytes[blockIndex] -= pieceLength
					downloader.Manager.PieceBytes[pieceData.PieceNumber] += pieceLength
					downloader.Downloaded += int64(pieceLength)
					downloader.writerChan <- pieceData
				}
				if downloader.Manager.IsPieceCompleted(pieceData.PieceNumber, &downloader.TorrentInfo) {
					downloader.Bitfield.Set(pieceData.PieceNumber, true)
				}
				downloader.Manager.BlockDownloading[blockIndex] = false
			}

			// If we receive at least one piece then we are good,
			// and we request more. If the opposite happens then we reconnect the peer.
			if piecesMessage.NumGood > 0 {

				downloader.startRequesting(piecesMessage.Peer)
			} else {
				piecesMessage.Peer.Disconnect()
				peers--
				go piecesMessage.Peer.EstablishFullConnection(downloader.connectionChan)
			}

		}
	}

	downloader.Status = COMPLETED
	return
}

func (downloader *Downloader) startRequesting(receivedPeer *peer.Peer) {
	now := time.Now()
	requestParams := []int{}
	for time.Since(now) <= 20*time.Microsecond {
		fiveBlocks := downloader.Manager.GetNextBlocksToDownload(receivedPeer, 5)
		if fiveBlocks == nil {
			break
		}
		smallParams := []int{}
		for block := 0; block < len(fiveBlocks); block++ {
			downloader.Manager.BlockDownloading[fiveBlocks[block]] = true
			smallParams = append(smallParams, downloader.Manager.BlockPiece[fiveBlocks[block]], downloader.Manager.BlockOffset[fiveBlocks[block]], downloader.Manager.BlockBytes[fiveBlocks[block]])
		}
		err := receivedPeer.WriteRequest(smallParams)
		requestParams = append(requestParams, smallParams...)
		if err != nil {
			break
		}
	}
	if requestParams != nil {
		go receivedPeer.ReadBlocks(downloader.requestChan, requestParams)
	} else {
		receivedPeer.Disconnect()
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
		TorrentInfo:    *torrentInfo,
		PeerId:         peerId,
		Bitfield:       &file_bitfield,
		writerChan:     make(chan file_writer.PieceData),
		requestChan:    make(chan peer.RequestCommunication),
		connectionChan: make(chan peer.ConnectionCommunication),
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
	downloader.Manager = piece_manager.New(torrentInfo)
	return downloader
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
