// Package downloader implements basic functions for downloading a torrent file
package downloader

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
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
	PIECE_LENGTH = 1 << 14
)

const (
	NOT_COMPLETED = iota
	DOWNLOADING
	COMPLETED
)

type Downloader struct {
	Tracker     tracker.Tracker
	TorrentInfo torrent_info.TorrentInfo
	LocalServer *local_server.LocalServer
	PeerId      string
	GoodPeers   []peer.Peer
	Bitfield    *bitfield.Bitfield
	Status      int
	Downloaded  int64

	piecesDownloading map[int]int
}

func (downloader Downloader) RequestPeers(comm chan peer.PeerCommunication, bytesUploaded, bytesDownloaded, bytesLeft int64) (peersCount int, err error) {

	// Request the peers , from the tracker
	// The first paramater is how many bytes uploaded , the second downloaded , and the third remaining size
	data, err := downloader.Tracker.RequestPeers(bytesUploaded, bytesDownloaded, bytesLeft)

	fmt.Println("Downloaded peers!")

	if err != nil {
		return 0, err
	}
	responseDictionary, responseIsDictionary := data.(*bencode.Dictionary)

	if !responseIsDictionary {
		return 0, err
	}

	peers, peersIsList := responseDictionary.Values[bencode.String{"peers"}].(*bencode.List)

	if !peersIsList {
		return 0, err
	}

	// At this point we have the peers as a list.

	for _, peerEntry := range peers.Values {
		peerData, peerDataIsDictionary := peerEntry.(*bencode.Dictionary)
		if peerDataIsDictionary {
			ip, ipIsString := peerData.Values[bencode.String{"ip"}].(*bencode.String)
			port, portIsNumber := peerData.Values[bencode.String{"port"}].(*bencode.Number)
			peerId, peerIdIsString := peerData.Values[bencode.String{"peer id"}].(*bencode.String)
			if ipIsString && portIsNumber && peerIdIsString {

				// We try to make a handshake with the peer.
				// Results are sent on the channel comm.

				newPeer := peer.New(&downloader.TorrentInfo, downloader.PeerId, ip.Value, int(port.Value))
				newPeer.RemotePeerId = peerId.Value
				newPeer.EstablishFullConnection(comm)
			}
		}
	}
	return len(peers.Values), nil
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

	startedTime := time.Now().Unix()

	downloader.RequestPeers(comm, 0, 0, 10000)

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

				downloader.Downloaded += int64(len(pieceBytes))

				writerChan <- file_writer.PieceData{pieceIndex, pieceOffset, pieceBytes}
				downloader.piecesDownloading[pieceIndex] += len(pieceBytes)
				howMany := 0
				for key, val := range downloader.piecesDownloading {
					fmt.Printf("%d -> %d / %d\n", key, val, downloader.TorrentInfo.FileInformations.PieceLength)
					howMany++
				}

				downloader.launchRequest(receivedPeer, pieceIndex, comm)

				fmt.Println(fmt.Sprintf("========= Downloading : %d Downloaded Pieces : %d / %d Downloaded : %d KB / %d KB Speed : %d KB/s (%.2f%%) =========", howMany, downloader.Bitfield.OneBits, downloader.Bitfield.Length, downloader.Downloaded, downloader.TorrentInfo.FileInformations.TotalLength, (downloader.Downloaded/(time.Now().Unix()-startedTime))/1024, 100*float64(downloader.Downloaded)/float64(downloader.TorrentInfo.FileInformations.TotalLength)))
			} else if msgID == peer.REQUEST && status != "OK" {

				fmt.Println(msg.StatusMessage)
				// mark the piece as not downloaded in order for another peer to pick it up
				// if it is an error , we received the exact parameters of what we requested , if we have some.
				// Right now , only the request have parameters
				pieceIndex := int(binary.BigEndian.Uint32(msg.BytesReceived[0:4]))
				downloader.Downloaded -= int64(downloader.piecesDownloading[pieceIndex])
				delete(downloader.piecesDownloading, pieceIndex)
				receivedPeer.Disconnect()
				receivedPeer.EstablishFullConnection(comm)

			} else if msgID == peer.FULL_CONNECTION && status == "OK" {

				nextPiece, err := downloader.GetNextPieceToDownload(receivedPeer)
				if err == nil {
					downloader.GoodPeers = append(downloader.GoodPeers, *receivedPeer)
					receivedPeer.RequestPiece(comm, nextPiece, 0, PIECE_LENGTH)
					downloader.piecesDownloading[nextPiece] = 0
				}

			} else if msgID == peer.FULL_CONNECTION && status != "OK" {
			}
		}
	}

	// We disconnect the peers so they dont remain connected after use
	for index, _ := range downloader.GoodPeers {
		defer downloader.GoodPeers[index].Disconnect()
	}

	downloader.Status = COMPLETED

	return
}

func (downloader Downloader) launchRequest(receivedPeer *peer.Peer, pieceIndex int, comm chan peer.PeerCommunication) {

	if pieceIndex == int(downloader.TorrentInfo.FileInformations.PieceCount-1) {
		// If it's the last piece , we need to treat it better.
		// The last piece has lesser size
		if downloader.TorrentInfo.FileInformations.PieceCount >= 2 {
			lastPieceLength := downloader.TorrentInfo.FileInformations.TotalLength - downloader.TorrentInfo.FileInformations.PieceLength*(downloader.TorrentInfo.FileInformations.PieceCount-1)
			if int64(downloader.piecesDownloading[pieceIndex]) >= lastPieceLength {
				delete(downloader.piecesDownloading, pieceIndex) // finished
				downloader.Bitfield.Set(pieceIndex, true)
				nextPiece, err := downloader.GetNextPieceToDownload(receivedPeer)
				if err != nil {
					return
				}
				downloader.piecesDownloading[nextPiece] = 0
				receivedPeer.RequestPiece(comm, nextPiece, 0, PIECE_LENGTH)
			} else {
				receivedPeer.RequestPiece(comm, pieceIndex, downloader.piecesDownloading[pieceIndex], PIECE_LENGTH)
			}
		}

	} else {

		if int64(downloader.piecesDownloading[pieceIndex]) >= downloader.TorrentInfo.FileInformations.PieceLength {
			delete(downloader.piecesDownloading, pieceIndex) // finished
			downloader.Bitfield.Set(pieceIndex, true)
			nextPiece, err := downloader.GetNextPieceToDownload(receivedPeer)
			if err != nil {
				return
			}
			downloader.piecesDownloading[nextPiece] = 0
			receivedPeer.RequestPiece(comm, nextPiece, 0, PIECE_LENGTH)
		} else {
			receivedPeer.RequestPiece(comm, pieceIndex, downloader.piecesDownloading[pieceIndex], PIECE_LENGTH)
		}
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

	bitfield := bitfield.New(int(torrentInfo.FileInformations.PieceCount))
	peerId := createPeerId()
	downloader := &Downloader{
		TorrentInfo: *torrentInfo,
		PeerId:      peerId,
		Bitfield:    &bitfield,
	}
	downloader.LocalServer = local_server.New(peerId)
	tracker := tracker.New(torrentInfo, downloader.LocalServer.Port, peerId)
	downloader.Tracker = tracker

	downloader.piecesDownloading = make(map[int]int)
	downloader.Status = NOT_COMPLETED

	return downloader
}

// Returns the ID of the next piece to download.
// This can use multiple strategies, e.g.
// Sequentially (NOT good, easy for development)
// or randomized (much better)
func (downloader *Downloader) GetNextPieceToDownload(peeR *peer.Peer) (int, error) {
	for i := 0; i < int(downloader.Bitfield.Length); i++ {
		downloaderHasPiece := downloader.Bitfield.At(i)
		peerHasPiece := peeR.BitfieldInfo.At(i)
		_, currentlyDownloading := downloader.piecesDownloading[i]

		if !downloaderHasPiece && peerHasPiece && !currentlyDownloading {
			return i, nil
		}
	}
	return -1, errors.New("Download complete")
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
