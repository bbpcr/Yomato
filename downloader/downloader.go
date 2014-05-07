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
	Trackers    []tracker.Tracker
	TorrentInfo torrent_info.TorrentInfo
	LocalServer *local_server.LocalServer
	PeerId      string
	GoodPeers   []peer.Peer
	Bitfield    *bitfield.Bitfield
	Status      int
	Downloaded  int64
	Speed       float64
	Manager     piece_manager.PieceManager

	DisconnectedPeers map[string]*peer.Peer
	ConnectedPeers    map[string]*peer.Peer

	writerChan     chan file_writer.PieceData
	connectionChan chan peer.ConnectionCommunication
	requestChan    chan peer.RequestCommunication
}

func (downloader Downloader) requestPeers(bytesUploaded int64, bytesDownloaded int64, bytesLeft int64, event int) {

	// Request the peers , from the tracker
	// The first paramater is how many bytes uploaded , the second downloaded , and the third remaining size.
	// The fourth param is the event.
	numPeers := 0
	for trackerIndex := 0; trackerIndex < len(downloader.Trackers); trackerIndex++ {

		trackerResponse := downloader.Trackers[trackerIndex].RequestPeers(bytesUploaded, bytesDownloaded, bytesLeft, event)

		for peerIndex := 0; peerIndex < len(trackerResponse.Peers); peerIndex++ {
			_, existsDC := downloader.DisconnectedPeers[trackerResponse.Peers[peerIndex].IP]
			_, existsCON := downloader.ConnectedPeers[trackerResponse.Peers[peerIndex].IP]
			if !existsDC && !existsCON {
				downloader.DisconnectedPeers[trackerResponse.Peers[peerIndex].IP] = &trackerResponse.Peers[peerIndex]
				go trackerResponse.Peers[peerIndex].EstablishFullConnection(downloader.connectionChan)
				numPeers++
			}
		}
	}
	fmt.Printf("%s %d trackers gave us new %d peers.\n", time.Now().Format("[2006.01.02 15:04:05]") , len(downloader.Trackers), numPeers)
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
	downloader.requestPeers(downloader.Downloaded, 0, downloader.TorrentInfo.FileInformations.TotalLength-downloader.Downloaded, tracker.DOWNLOAD_STARTED)

	ticker := time.NewTicker(time.Second * 3)
	go func() {
		var seconds int = 0
		var lastDownloaded int64 = 0
		for _ = range ticker.C {
			seconds ++
			downloader.Speed = float64(downloader.Downloaded-lastDownloaded) / 1024.0
			downloader.Speed /= 3
			lastDownloaded = downloader.Downloaded
			fmt.Println(time.Now().Format("[2006.01.02 15:04:05]"),fmt.Sprintf("Peers : %d / %d Downloaded Pieces : %d / %d Downloaded : %d KB / %d KB (%.2f%%) Speed : %.2f KB/s Elapsed : %.2f seconds ", len(downloader.ConnectedPeers) , len(downloader.ConnectedPeers) + len(downloader.DisconnectedPeers), downloader.Bitfield.OneBits, downloader.Bitfield.Length, downloader.Downloaded, downloader.TorrentInfo.FileInformations.TotalLength, 100.0*float64(downloader.Downloaded)/float64(downloader.TorrentInfo.FileInformations.TotalLength), downloader.Speed, time.Since(startedTime).Seconds()))
			if seconds == 4 {
				go downloader.requestPeers(downloader.Downloaded, 0, downloader.TorrentInfo.FileInformations.TotalLength-downloader.Downloaded, tracker.NONE)
				seconds = 0
			}
		}
	}()

	defer downloader.requestPeers(downloader.Downloaded, 0, downloader.TorrentInfo.FileInformations.TotalLength-downloader.Downloaded, tracker.DOWNLOAD_STOPPED)
	defer ticker.Stop()

	for downloader.Downloaded < downloader.TorrentInfo.FileInformations.TotalLength {
		select {
		
		case connectionMessage, _ := <-downloader.connectionChan:
			if connectionMessage.StatusMessage == "OK" {
				downloader.ConnectedPeers[connectionMessage.Peer.IP] = connectionMessage.Peer
				delete(downloader.DisconnectedPeers, connectionMessage.Peer.IP)
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
			
			if piecesMessage.NumGood > 0 {
				downloader.startRequesting(piecesMessage.Peer)
			} else {
				piecesMessage.Peer.Disconnect()
				downloader.DisconnectedPeers[piecesMessage.Peer.IP] = piecesMessage.Peer				
				delete(downloader.ConnectedPeers , piecesMessage.Peer.IP)
				go piecesMessage.Peer.EstablishFullConnection(downloader.connectionChan)
			}

			// If we receive at least one piece then we are good,
			// and we request more. If the opposite happens then we reconnect the peer.
		}
	}	

	downloader.Status = COMPLETED
	ticker.Stop()
	downloader.requestPeers(downloader.Downloaded, 0, downloader.TorrentInfo.FileInformations.TotalLength-downloader.Downloaded, tracker.DOWNLOAD_COMPLETED)
	return
}

func (downloader *Downloader) startRequesting(receivedPeer *peer.Peer) {
	requestParams := []int{}
	for i := 0; i < 2; i++ {
		fiveBlocks := downloader.Manager.GetNextBlocksToDownload(receivedPeer, 5)
		if fiveBlocks != nil {
			smallParams := []int{}
			for block := 0; block < len(fiveBlocks); block++ {
				downloader.Manager.BlockDownloading[fiveBlocks[block]] = true
				smallParams = append(smallParams, downloader.Manager.BlockPiece[fiveBlocks[block]], downloader.Manager.BlockOffset[fiveBlocks[block]], downloader.Manager.BlockBytes[fiveBlocks[block]])
			}
			err := receivedPeer.WriteRequest(smallParams)
			requestParams = append(requestParams, smallParams...)
			if err != nil {
				return
			}
		}
	}
	if requestParams != nil {
		go receivedPeer.ReadBlocks(downloader.requestChan, requestParams)
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
		TorrentInfo:       *torrentInfo,
		PeerId:            peerId,
		Bitfield:          &file_bitfield,
		
		DisconnectedPeers: make(map[string]*peer.Peer),
		ConnectedPeers:    make(map[string]*peer.Peer),
		
		writerChan:        make(chan file_writer.PieceData),
		requestChan:       make(chan peer.RequestCommunication),
		connectionChan:    make(chan peer.ConnectionCommunication),
	}
	downloader.LocalServer = local_server.New(peerId)
	downloader.Trackers = make([]tracker.Tracker, 1)

	mainTracker := tracker.New(torrentInfo.AnnounceUrl, torrentInfo, downloader.LocalServer.Port, peerId)
	downloader.Trackers[0] = mainTracker

	for _, announcerUrl := range torrentInfo.AnnounceList {
		tracker := tracker.New(announcerUrl, torrentInfo, downloader.LocalServer.Port, peerId)
		if tracker.AnnounceUrl != mainTracker.AnnounceUrl {
			downloader.Trackers = append(downloader.Trackers, tracker)
		}
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
