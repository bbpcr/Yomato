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
	"github.com/bbpcr/Yomato/peer_manager"
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
	MAX_ACTIVE_REQUESTS    = 70
	MAX_ACTIVE_CONNECTIONS = 150
	MAX_NEW_CONNECTIONS    = 20
	MIN_ACTIVE_CONNECTIONS = 10
)

const (
	UNCHOKE_DURATION    = 30 * time.Second
	RECONNECT_DURATION  = 15 * time.Second
	KEEP_ALIVE_DURATION = 60 * time.Second
)

type Downloader struct {
	Trackers      []tracker.Tracker
	TorrentInfo   torrent_info.TorrentInfo
	LocalServer   *local_server.LocalServer
	PeerId        string
	Bitfield      *bitfield.Bitfield
	Status        int
	Downloaded    int64
	Speed         float64
	PiecesManager *piece_manager.PieceManager
	PeersManager  *peer_manager.PeerManager
	fileWriter    *file_writer.Writer

	connectionChan chan peer.ConnectionCommunication
}

func (downloader *Downloader) requestPeers(event int) {

	// Request the peers , from the tracker
	// The first paramater is how many bytes uploaded , the second downloaded , and the third remaining size.
	// The fourth param is the event.
	numPeers := 0
	bytesDownloaded := downloader.PiecesManager.CalculateDownloaded()
	bytesLeft := downloader.TorrentInfo.FileInformations.TotalLength - bytesDownloaded
	bytesUploaded := int64(0)
	for trackerIndex := 0; trackerIndex < len(downloader.Trackers); trackerIndex++ {

		trackerResponse := downloader.Trackers[trackerIndex].RequestPeers(bytesUploaded, bytesDownloaded, bytesLeft, event)

		for peerIndex := 0; peerIndex < len(trackerResponse.Peers); peerIndex++ {
			if !downloader.PeersManager.Exists(&trackerResponse.Peers[peerIndex]) {
				downloader.PeersManager.SetPeerAsDisconnected(&trackerResponse.Peers[peerIndex])
				go trackerResponse.Peers[peerIndex].EstablishFullConnection(downloader.connectionChan, downloader.Bitfield)
				numPeers++
			}
		}
	}
	fmt.Printf("%s %d trackers gave us new %d peers.\n", time.Now().Format("[2006.01.02 15:04:05]"), len(downloader.Trackers), numPeers)
}

func (downloader *Downloader) checkExistingFiles() {
	fmt.Println(time.Now().Format("[2006.01.02 15:04:05]"), "Computing missing pieces..")
	startTime := time.Now()
	missing := 0
	for pieceIndex := 0; pieceIndex < int(downloader.TorrentInfo.FileInformations.PieceCount); pieceIndex++ {
		if downloader.fileWriter.CheckSha1Sum(int64(pieceIndex)) {
			downloader.PiecesManager.RemovePieceFromDownload(pieceIndex, &downloader.TorrentInfo)
			downloader.Bitfield.Set(pieceIndex, true)
		} else {
			missing++
		}
	}
	fmt.Println(time.Now().Format("[2006.01.02 15:04:05]"), "Computed missing pieces in", fmt.Sprintf("%.2fs", time.Since(startTime).Seconds()))
	fmt.Println(time.Now().Format("[2006.01.02 15:04:05]"), "Have", missing, "missing pieces")

}

func (downloader *Downloader) ScanForUnchoke(seeder *peer.Peer) {

	if seeder.Active || seeder.Downloading {
		return
	}

	seeder.Active = true

	time.Sleep(1 * time.Second)
	startTime := time.Now()
	for time.Since(startTime) < UNCHOKE_DURATION && seeder.PeerChoking && seeder.Status == peer.CONNECTED {
		err := seeder.SendInterested()
		if err != nil {
			seeder.Disconnect()
			downloader.PeersManager.SetPeerAsDisconnected(seeder)
			return
		}
		seeder.ReadMessages(1, 5*time.Second)
	}
	if !seeder.PeerChoking {
		numRequesting := downloader.PeersManager.CountDownloadingPeers()
		if numRequesting < MAX_ACTIVE_REQUESTS {
			go downloader.DownloadFromPeer(seeder)
		}
	}
	seeder.Active = false
}

// This function , start downloading from a peer.
func (downloader *Downloader) DownloadFromPeer(seeder *peer.Peer) {

	if seeder.Downloading || seeder.Active {
		return
	}
	seeder.Downloading = true

	for seeder.Status == peer.CONNECTED {

		blocks := downloader.PiecesManager.GetNextBlocksToDownload(seeder, 10)
		if blocks == nil {
			break
		}
		smallParams := []int{}
		for block := 0; block < len(blocks); block++ {
			downloader.PiecesManager.SetBlockDownloading(blocks[block], true)
			index, offset, length := downloader.PiecesManager.MakeRequest(blocks[block])
			smallParams = append(smallParams, index, offset, length)
		}
		err := seeder.WriteRequest(smallParams)
		if err != nil {
			break
		}

		pieces := seeder.ReadMessages(len(blocks), 3*time.Second)

		if len(pieces) > 0 {
			for _, pieceData := range pieces {
				err := downloader.PiecesManager.UpdatePiece(pieceData)
				downloader.Downloaded += int64(len(pieceData.Piece))
				if err == nil {
					downloader.fileWriter.WritePiece(pieceData)
				}
				if downloader.PiecesManager.IsPieceCompleted(pieceData.PieceNumber, &downloader.TorrentInfo) {
					if !downloader.Bitfield.At(pieceData.PieceNumber) {
						if downloader.fileWriter.CheckSha1Sum(int64(pieceData.PieceNumber)) {
							downloader.Bitfield.Set(pieceData.PieceNumber, true)
						} else {
							fmt.Println("Dropped piece ", pieceData.PieceNumber)
							downloader.PiecesManager.AddPieceToDownload(pieceData.PieceNumber, &downloader.TorrentInfo)
						}
					}
				}
				downloader.PiecesManager.SetPieceDownloading(pieceData, false)
			}
			for block := 0; block < len(blocks); block++ {
				downloader.PiecesManager.SetBlockDownloading(blocks[block], false)
			}
		} else {
			for block := 0; block < len(blocks); block++ {
				downloader.PiecesManager.SetBlockDownloading(blocks[block], false)
			}
			break
		}
		if seeder.PeerChoking {
			break
		}
	}

	// If the peer gets choked , we try to
	// wait and see if it gets unchoked.
	// If another error occurs we disconnect the peer.
	if seeder.PeerChoking {
		err := seeder.SendUninterested()
		if err != nil {
			seeder.Disconnect()
			downloader.PeersManager.SetPeerAsDisconnected(seeder)
		} else {
			go downloader.ScanForUnchoke(seeder)
		}
	} else {
		seeder.Disconnect()
		downloader.PeersManager.SetPeerAsDisconnected(seeder)
	}

	// Whatever happens , we need to keep requesting,
	// so we replace the current seed with another seed, if we can
	var bestUnchoked *peer.Peer = nil
	var bestChoked *peer.Peer = nil
	// We need to find the best peer who isn't unchoked and currently downloading
	// and we need to find the best peer who is choked so we can try to make more usable peers
	for _, connectedPeer := range downloader.PeersManager.GetConnectedPeers() {

		if seeder.IP != connectedPeer.IP && !connectedPeer.Downloading && !connectedPeer.PeerChoking {
			if bestUnchoked == nil {
				bestUnchoked = connectedPeer
			} else if bestUnchoked.ConnectTime > connectedPeer.ConnectTime {
				bestUnchoked = connectedPeer
			}
		}

		if seeder.IP != connectedPeer.IP && !connectedPeer.Downloading && connectedPeer.PeerChoking && !connectedPeer.Active {
			if bestChoked == nil {
				bestChoked = connectedPeer
			} else if bestChoked.ConnectTime > connectedPeer.ConnectTime {
				bestChoked = connectedPeer
			}
		}
	}

	if bestUnchoked != nil {
		if downloader.PeersManager.CountConnectedPeers() < MAX_ACTIVE_REQUESTS {
			go downloader.DownloadFromPeer(bestUnchoked)
		}
	}

	if bestChoked != nil {
		go downloader.ScanForUnchoke(bestChoked)
	}
	seeder.Downloading = false
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
	downloader.fileWriter = file_writer.New(filepath.Join(cwd, "TorrentDownloads"), downloader.TorrentInfo)
	defer downloader.fileWriter.CloseFiles()
	downloader.checkExistingFiles()

	downloader.requestPeers(tracker.DOWNLOAD_STARTED)

	ticker := time.NewTicker(time.Second * 2)
	defer ticker.Stop()
	reconnectTicker := time.NewTicker(RECONNECT_DURATION)
	defer reconnectTicker.Stop()
	keepAliveTicker := time.NewTicker(KEEP_ALIVE_DURATION)
	defer keepAliveTicker.Stop()

	defer downloader.requestPeers(tracker.DOWNLOAD_STOPPED)

	startedTime := time.Now()
	go func() {
		var seconds int = 0
		var lastDownloaded int64 = 0
		for _ = range ticker.C {
			seconds += 2
			downloader.Speed = float64(downloader.Downloaded-lastDownloaded) / 1024.0
			downloader.Speed /= 2
			lastDownloaded = downloader.Downloaded
			numRequesting := downloader.PeersManager.CountDownloadingPeers()
			fmt.Println(time.Now().Format("[2006.01.02 15:04:05]"), fmt.Sprintf("Peers : %d / %d [Total %d / %d] Downloaded Pieces : %d / %d (%.2f%%) Speed : %.2f KB/s Elapsed : %.2f seconds ", numRequesting, downloader.PeersManager.CountConnectedPeers(), downloader.PeersManager.CountAlivePeers(), downloader.PeersManager.CountAllPeers(), downloader.Bitfield.OneBits, downloader.Bitfield.Length, float64(downloader.Bitfield.OneBits)*100.0/float64(downloader.Bitfield.Length), downloader.Speed, time.Since(startedTime).Seconds()))
			if seconds == 200 {
				downloader.requestPeers(tracker.NONE)
				seconds = 0
			}
		}
	}()

	for downloader.Bitfield.OneBits < downloader.Bitfield.Length {
		select {

		case _ = <-keepAliveTicker.C:

			// This ticker is called every KEEP_ALIVE_DURATION seconds
			// We send a keep alive message , and we try to unchoke the peers that are choked
			numKeptAlive := 0
			for _, connectedPeer := range downloader.PeersManager.GetConnectedPeers() {
				if !connectedPeer.Downloading {
					if err := connectedPeer.SendKeepAlive(); err != nil {
						connectedPeer.Disconnect()
						downloader.PeersManager.SetPeerAsConnected(connectedPeer)
					}
					numKeptAlive++
				}
			}

			numUnchoking := 0
			for _, connectedPeer := range downloader.PeersManager.GetConnectedPeers() {
				if !connectedPeer.Active && connectedPeer.PeerChoking {
					go downloader.ScanForUnchoke(connectedPeer)
					numUnchoking++
				}
			}
			fmt.Println(time.Now().Format("[2006.01.02 15:04:05]"), fmt.Sprintf("Sent keep alive to %d connected peers and unchoking %d connected peers", numKeptAlive, numUnchoking))

		case _ = <-reconnectTicker.C:
			// This ticker is called every RECONNECT_DURATION seconds
			// If we have less than MIN_ACTIVE_CONNECTIONS peers connected , we reconnect all of them.
			// If that doesnt happen then we choose 3 disconnected peers , and we try to connect them.
			// Also we try to make more requests.
			connectedPeersCount := downloader.PeersManager.CountConnectedPeers()
			if connectedPeersCount < MIN_ACTIVE_CONNECTIONS {

				for _, alivePeer := range downloader.PeersManager.GetAlivePeers() {
					if alivePeer.Status == peer.DISCONNECTED {
						go alivePeer.EstablishFullConnection(downloader.connectionChan, downloader.Bitfield)
					}
				}
				fmt.Println(time.Now().Format("[2006.01.02 15:04:05]"), "Reconnecting all alive peers, because of low connections")

			} else if connectedPeersCount < MAX_ACTIVE_CONNECTIONS {

				newConnections := 0
				for _, alivePeer := range downloader.PeersManager.GetAlivePeers() {
					if alivePeer.Status == peer.DISCONNECTED {
						go alivePeer.EstablishFullConnection(downloader.connectionChan, downloader.Bitfield)
						newConnections++
						if newConnections == MAX_NEW_CONNECTIONS {
							break
						}
					}
				}
				fmt.Println(time.Now().Format("[2006.01.02 15:04:05]"), fmt.Sprintf("Trying %d new connections", newConnections))
			}

			numDownloading := downloader.PeersManager.CountDownloadingPeers()
			for _, connectedPeer := range downloader.PeersManager.GetAlivePeers() {
				if numDownloading < MAX_ACTIVE_REQUESTS && !connectedPeer.PeerChoking && !connectedPeer.Downloading {
					numDownloading++
					go downloader.DownloadFromPeer(connectedPeer)
				}
			}

		case connectionMessage, _ := <-downloader.connectionChan:

			if connectionMessage.StatusMessage == "OK" {

				if downloader.PeersManager.CountConnectedPeers() < MAX_ACTIVE_CONNECTIONS {
					downloader.PeersManager.SetPeerAsConnected(connectionMessage.Peer)
				} else {
					// find the worst peer, who is choked
					var worstPeer *peer.Peer = nil
					for _, connectedPeer := range downloader.PeersManager.GetConnectedPeers() {
						if worstPeer != nil {
							if worstPeer.ConnectTime < connectedPeer.ConnectTime && connectedPeer.PeerChoking {
								worstPeer = connectedPeer
							}
						} else if connectedPeer.PeerChoking {
							worstPeer = connectedPeer
						}
					}

					if worstPeer != nil && worstPeer.ConnectTime > connectionMessage.Peer.ConnectTime {
						worstPeer.Disconnect()
						downloader.PeersManager.SetPeerAsDisconnected(worstPeer)
						downloader.PeersManager.SetPeerAsConnected(connectionMessage.Peer)
					} else {
						connectionMessage.Peer.Disconnect()
					}
				}

				if connectionMessage.Peer.Status != peer.DISCONNECTED {

					numDownloading := downloader.PeersManager.CountDownloadingPeers()

					if connectionMessage.Peer.PeerChoking {
						go downloader.ScanForUnchoke(connectionMessage.Peer)
					} else {
						if numDownloading < MAX_ACTIVE_REQUESTS {
							go downloader.DownloadFromPeer(connectionMessage.Peer)
						} else {
							//connectionMessage.Peer.SendUninterested()
						}
					}
				}
				downloader.PeersManager.SetPeerAsAlive(connectionMessage.Peer)
			}
		}
	}

	downloader.requestPeers(tracker.DOWNLOAD_COMPLETED)

	downloader.Status = COMPLETED
	ticker.Stop()
	reconnectTicker.Stop()
	keepAliveTicker.Stop()
	fmt.Println(time.Now().Format("[2006.01.02 15:04:05]"), fmt.Sprintf("Download completeted in %.2f seconds, with average speed %.2f KB/s\n", time.Since(startedTime).Seconds(), float64(downloader.Downloaded)/time.Since(startedTime).Seconds()/1024.0))
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

		PiecesManager: piece_manager.New(torrentInfo),
		PeersManager:  peer_manager.New(),

		connectionChan: make(chan peer.ConnectionCommunication),
		Status:         NOT_COMPLETED,
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
