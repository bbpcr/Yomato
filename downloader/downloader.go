// Package downloader implements basic functions for downloading a torrent file
package downloader

import (
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bbpcr/Yomato/bencode"
	"github.com/bbpcr/Yomato/bitfield"
	"github.com/bbpcr/Yomato/database"
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

const (
	MAX_ACTIVE_REQUESTS    = 30
	MAX_ACTIVE_CONNECTIONS = 100
	MAX_NEW_CONNECTIONS    = 20
	MIN_ACTIVE_CONNECTIONS = 10
)

const (
	UNCHOKE_DURATION    = 30 * time.Second
	RECONNECT_DURATION  = 15 * time.Second
	KEEP_ALIVE_DURATION = 60 * time.Second
)

type Downloader struct {
	Trackers    []tracker.Tracker
	TorrentInfo torrent_info.TorrentInfo
	LocalServer *local_server.LocalServer
	PeerId      string
	GoodPeers   []peer.Peer
	Bitfield    *bitfield.Bitfield
	Status      int
	Speed       float64
	Manager     *piece_manager.PieceManager

	DisconnectedPeers map[string]*peer.Peer
	ConnectedPeers    map[string]*peer.Peer
	AlivePeers        map[string]*peer.Peer

	writerChan     chan file_writer.PieceData
	writeDoneChan  chan file_writer.PieceData
	connectionChan chan peer.ConnectionCommunication
	requestChan    chan peer.RequestCommunication

	peerLocker  sync.Mutex
	torrentPath string
	model       *database.Model
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
	fmt.Printf("%s %d trackers gave us new %d peers.\n", time.Now().Format("[2006.01.02 15:04:05]"), len(downloader.Trackers), numPeers)
}

func (downloader *Downloader) ScanForUnchoke(seeder *peer.Peer) {

	time.Sleep(1 * time.Second)
	startTime := time.Now()
	for time.Since(startTime) < UNCHOKE_DURATION && seeder.PeerChoking && seeder.Status == peer.CONNECTED {
		err := seeder.SendInterested()
		if err != nil {
			break
		}
		seeder.ReadMessages(1, 5*time.Second)
	}

	if seeder.PeerChoking {
		downloader.peerLocker.Lock()
		delete(downloader.ConnectedPeers, seeder.IP)
		downloader.DisconnectedPeers[seeder.IP] = seeder
		downloader.peerLocker.Unlock()
	} else {
		numRequesting := 0
		for _, connectedPeer := range downloader.ConnectedPeers {
			if connectedPeer.Requesting {
				numRequesting++
			}
		}
		if numRequesting < MAX_ACTIVE_REQUESTS {
			go downloader.DownloadFromPeer(seeder)
		}
	}
}

func (downloader *Downloader) DownloadFromPeer(seeder *peer.Peer) {

	if seeder.Requesting {
		return
	}
	seeder.Requesting = true

	for seeder.Status == peer.CONNECTED {

		blocks := downloader.Manager.GetNextBlocksToDownload(&seeder.BitfieldInfo, 10)
		if blocks == nil {
			break
		}
		smallParams := []int{}
		for block := 0; block < len(blocks); block++ {
			downloader.Manager.SetBlockDownloading(blocks[block], true)
			index, offset, length := downloader.Manager.MakeRequest(blocks[block])
			smallParams = append(smallParams, index, offset, length)
		}
		err := seeder.WriteRequest(smallParams)
		if err != nil {
			for _, block := range blocks {
				downloader.Manager.SetBlockDownloading(block, false)
			}
			break
		}

		pieces := seeder.ReadMessages(len(blocks), 3*time.Second)

		if len(pieces) > 0 {
			for _, pieceData := range pieces {
				blockIndex := downloader.Manager.GetBlockIndex(pieceData.PieceNumber, pieceData.Offset)
				if blockIndex == -1 {
					continue
				}

				blockLength := len(pieceData.Piece)
				if downloader.Manager.BlockSizeCoresponds(blockIndex, blockLength) {
					downloader.Manager.MarkBlockDownloaded(blockIndex)
					downloader.writerChan <- pieceData
				}

				if downloader.Manager.IsPieceCompleted(pieceData.PieceNumber) {
					downloader.Bitfield.Set(pieceData.PieceNumber, true)
				}
			}
			for block := 0; block < len(blocks); block++ {
				downloader.Manager.SetBlockDownloading(blocks[block], false)
			}
		} else {
			for block := 0; block < len(blocks); block++ {
				downloader.Manager.SetBlockDownloading(blocks[block], false)
			}
			break
		}
		if seeder.PeerChoking {
			break
		}
	}

	if seeder.PeerChoking {
		seeder.SendUninterested()
		go downloader.ScanForUnchoke(seeder)
	} else {
		seeder.Disconnect()
		downloader.peerLocker.Lock()
		delete(downloader.ConnectedPeers, seeder.IP)
		downloader.DisconnectedPeers[seeder.IP] = seeder
		downloader.peerLocker.Unlock()
	}

	numRequesting := 0
	var bestUnchoked *peer.Peer = nil
	var bestChoked *peer.Peer = nil
	for _, connectedPeer := range downloader.ConnectedPeers {

		if seeder.IP != connectedPeer.IP && !connectedPeer.Requesting && !connectedPeer.PeerChoking {
			if bestUnchoked == nil {
				bestUnchoked = connectedPeer
			} else if bestUnchoked.ConnectTime > connectedPeer.ConnectTime {
				bestUnchoked = connectedPeer
			}
		}

		if seeder.IP != connectedPeer.IP && !connectedPeer.Requesting && connectedPeer.PeerChoking {
			if bestChoked == nil {
				bestChoked = connectedPeer
			} else if bestChoked.ConnectTime > connectedPeer.ConnectTime {
				bestChoked = connectedPeer
			}
		}

		if connectedPeer.Requesting {
			numRequesting++
		}
	}

	if bestUnchoked != nil {
		if numRequesting < MAX_ACTIVE_REQUESTS {
			go downloader.DownloadFromPeer(bestUnchoked)
		}
	} else if bestChoked != nil {
		if numRequesting < MAX_ACTIVE_REQUESTS {
			go downloader.DownloadFromPeer(bestChoked)
		}
	}
	seeder.Requesting = false
}

// StartDownloading downloads the motherfucker
func (downloader *Downloader) StartDownloading() {

	if downloader.Status == DOWNLOADING {
		return
	}
	if err := downloader.model.Connect(); err != nil {
		panic(err)
	}

	defer downloader.model.Disconnect()

	downloader.Manager = piece_manager.New(&downloader.TorrentInfo, downloader.model)
	for i := 0; i < int(downloader.Bitfield.Length); i++ {
		if downloader.Manager.IsPieceCompleted(i) {
			downloader.Bitfield.Set(i, true)
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	writer := file_writer.New(filepath.Join(cwd, "TorrentDownloads"), downloader.TorrentInfo)
	go writer.StartWriting(downloader.writerChan, downloader.writeDoneChan)

	downloader.requestPeers(downloader.Manager.BytesDownloaded(), 0, downloader.TorrentInfo.FileInformations.TotalLength-downloader.Manager.BytesDownloaded(), tracker.DOWNLOAD_STARTED)

	ticker := time.NewTicker(time.Second * 2)
	defer ticker.Stop()
	reconnectTicker := time.NewTicker(RECONNECT_DURATION)
	defer reconnectTicker.Stop()
	keepAliveTicker := time.NewTicker(KEEP_ALIVE_DURATION)
	defer keepAliveTicker.Stop()
	defer downloader.requestPeers(downloader.Manager.BytesDownloaded(), 0, downloader.TorrentInfo.FileInformations.TotalLength-downloader.Manager.BytesDownloaded(), tracker.DOWNLOAD_STOPPED)

	startedTime := time.Now()
	go func() {
		var seconds int = 0
		var lastDownloaded int64 = downloader.Manager.BytesDownloaded()
		downloader.model.BeginTransaction()
		for _ = range ticker.C {
			seconds += 2
			downloader.Speed = float64(downloader.Manager.BytesDownloaded()-lastDownloaded) / 1024.0
			downloader.Speed /= 2
			lastDownloaded = downloader.Manager.BytesDownloaded()
			numRequesting := 0
			for _, connectedPeer := range downloader.ConnectedPeers {
				if connectedPeer.Requesting {
					numRequesting++
				}
			}
			downloader.model.AddTimeDownloaded(2)
			fmt.Println(time.Now().Format("[2006.01.02 15:04:05]"), fmt.Sprintf("Peers : %d / %d [Total %d / %d] Downloaded Pieces : %d / %d Downloaded : %d KB / %d KB (%.2f%%) Speed : %.2f KB/s Elapsed : %.2f seconds ", numRequesting, len(downloader.ConnectedPeers), len(downloader.AlivePeers), len(downloader.ConnectedPeers)+len(downloader.DisconnectedPeers), downloader.Bitfield.OneBits, downloader.Bitfield.Length, downloader.Manager.BytesDownloaded(), downloader.TorrentInfo.FileInformations.TotalLength, 100.0*float64(downloader.Manager.BytesDownloaded())/float64(downloader.TorrentInfo.FileInformations.TotalLength), downloader.Speed, time.Since(startedTime).Seconds()))
			if seconds == 200 {
				downloader.requestPeers(downloader.Manager.BytesDownloaded(), 0, downloader.TorrentInfo.FileInformations.TotalLength-downloader.Manager.BytesDownloaded(), tracker.NONE)
				seconds = 0
			}

			downloader.model.CommitTransaction()
			downloader.model.BeginTransaction()
		}

		downloader.model.CommitTransaction()
	}()

	defer ticker.Stop()

	go func() {
		for pieceData := range downloader.writeDoneChan {
			blockIndex := downloader.Manager.GetBlockIndex(pieceData.PieceNumber, pieceData.Offset)
			if err := downloader.model.MarkBlockHave(blockIndex); err != nil {
				panic(err)
			}
		}
	}()

	for {
		if downloader.Manager.BytesDownloaded() >= downloader.TorrentInfo.FileInformations.TotalLength {
			// check SHA sums for all pieces to confirm that the download is done. If there are any
			// corrupted pieces, re-download them
			fmt.Printf("Download almost done; checking SHA-1 sums.\n")
			corruptedPieces := writer.CheckSha1Sums()
			if len(corruptedPieces) == 0 {
				fmt.Printf("File downloaded correctly\n")
				break
			} else {
				for _, piece := range corruptedPieces {
					pieceSize := downloader.TorrentInfo.FileInformations.PieceLength
					if int64(piece) == downloader.TorrentInfo.FileInformations.PieceCount-1 {
						// last piece, compute actual piece size
						pieceSize = downloader.TorrentInfo.FileInformations.TotalLength - downloader.TorrentInfo.FileInformations.PieceLength*(downloader.TorrentInfo.FileInformations.PieceCount-1)
					}
					downloader.Bitfield.Set(int(piece), false)

					downloader.Manager.UnmarkPieceDownloaded(int(piece), int(pieceSize))
				}
			}
		}

		select {

		case _ = <-keepAliveTicker.C:

			numSent := 0
			for _, connectedPeer := range downloader.ConnectedPeers {
				if !connectedPeer.Requesting {
					go func() {
						if err := connectedPeer.SendKeepAlive(); err != nil {
							downloader.peerLocker.Lock()
							downloader.DisconnectedPeers[connectedPeer.IP] = connectedPeer
							delete(downloader.ConnectedPeers, connectedPeer.IP)
							downloader.peerLocker.Unlock()
						}
					}()
					numSent++
				}
			}
			fmt.Println(time.Now().Format("[2006.01.02 15:04:05]"), fmt.Sprintf("Sending keep alive to %d connected peers", numSent))

		case _ = <-reconnectTicker.C:
			// This ticker is called every 5 seconds
			// If we have less than MIN_ACTIVE_CONNECTIONS peers connected , we reconnect all of them.
			// If that doesnt happen then we choose 3 disconnected peers , and we try to connect them.
			connectedPeersCount := len(downloader.ConnectedPeers)
			if connectedPeersCount < MIN_ACTIVE_CONNECTIONS {
				for _, alivePeer := range downloader.AlivePeers {
					if alivePeer.Status == peer.DISCONNECTED {
						go alivePeer.EstablishFullConnection(downloader.connectionChan)
					}
				}
				fmt.Println(time.Now().Format("[2006.01.02 15:04:05]"), "Reconnecting all alive peers, because of low connections")

			} else if connectedPeersCount < MAX_ACTIVE_CONNECTIONS {

				newConnections := 0
				for _, alivePeer := range downloader.AlivePeers {
					if alivePeer.Status == peer.DISCONNECTED {
						go alivePeer.EstablishFullConnection(downloader.connectionChan)
						newConnections++
						if newConnections == MAX_NEW_CONNECTIONS {
							break
						}
					}
				}
				fmt.Println(time.Now().Format("[2006.01.02 15:04:05]"), fmt.Sprintf("Trying %d new connections", newConnections))
			}

			numRequesting := 0
			for _, connectedPeer := range downloader.ConnectedPeers {
				if connectedPeer.Requesting {
					numRequesting++
				}

			}
			for _, connectedPeer := range downloader.ConnectedPeers {
				if numRequesting < MAX_ACTIVE_REQUESTS && !connectedPeer.PeerChoking && !connectedPeer.Requesting {
					numRequesting++
					go downloader.DownloadFromPeer(connectedPeer)
				}
			}

		case connectionMessage, _ := <-downloader.connectionChan:

			if connectionMessage.StatusMessage == "OK" {

				if len(downloader.ConnectedPeers) < MAX_ACTIVE_CONNECTIONS {
					downloader.peerLocker.Lock()
					downloader.ConnectedPeers[connectionMessage.Peer.IP] = connectionMessage.Peer
					delete(downloader.DisconnectedPeers, connectionMessage.Peer.IP)
					downloader.peerLocker.Unlock()
				} else {
					// find the worst peer, who isn't requesting
					var worstPeer *peer.Peer = nil
					for _, connectedPeer := range downloader.ConnectedPeers {
						if worstPeer != nil {
							if worstPeer.ConnectTime < connectedPeer.ConnectTime && connectedPeer.PeerChoking {
								worstPeer = connectedPeer
							}
						} else if connectedPeer.PeerChoking {
							worstPeer = connectedPeer
						}
					}

					if worstPeer != nil && worstPeer.ConnectTime > connectionMessage.Peer.ConnectTime {
						downloader.peerLocker.Lock()
						worstPeer.Disconnect()
						downloader.DisconnectedPeers[worstPeer.IP] = worstPeer
						delete(downloader.ConnectedPeers, worstPeer.IP)
						downloader.ConnectedPeers[connectionMessage.Peer.IP] = connectionMessage.Peer
						delete(downloader.DisconnectedPeers, connectionMessage.Peer.IP)
						downloader.peerLocker.Unlock()
					} else {
						connectionMessage.Peer.Disconnect()
					}
				}

				if connectionMessage.Peer.Status != peer.DISCONNECTED {

					numRequesting := 0
					for _, connectedPeer := range downloader.ConnectedPeers {
						if connectedPeer.Requesting {
							numRequesting++
						}
					}

					if numRequesting < MAX_ACTIVE_REQUESTS {
						go downloader.DownloadFromPeer(connectionMessage.Peer)
					} else {
						//connectionMessage.Peer.SendUninterested()
					}
				}

				downloader.AlivePeers[connectionMessage.Peer.IP] = connectionMessage.Peer
			} else {
				delete(downloader.AlivePeers, connectionMessage.Peer.IP)
			}
		}
	}

	close(downloader.writeDoneChan)

	downloader.Status = COMPLETED
	ticker.Stop()
	reconnectTicker.Stop()
	keepAliveTicker.Stop()

	totalTime, err := downloader.model.TimeUsed()
	if err != nil {
		panic(err)
	}

	fmt.Printf("Download completed in %d seconds, with average speed %.2f KB/s\n", totalTime, float64(downloader.Manager.BytesDownloaded())/float64(totalTime)/1024.0)

	defer downloader.requestPeers(downloader.Manager.BytesDownloaded(), 0, downloader.TorrentInfo.FileInformations.TotalLength-downloader.Manager.BytesDownloaded(), tracker.DOWNLOAD_COMPLETED)
	return
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

		DisconnectedPeers: make(map[string]*peer.Peer),
		ConnectedPeers:    make(map[string]*peer.Peer),
		AlivePeers:        make(map[string]*peer.Peer),

		writerChan:     make(chan file_writer.PieceData, 5),
		writeDoneChan:  make(chan file_writer.PieceData, 5),
		requestChan:    make(chan peer.RequestCommunication),
		connectionChan: make(chan peer.ConnectionCommunication),
		torrentPath:    torrent_path,
		model:          database.NewModel(string(data)),
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
