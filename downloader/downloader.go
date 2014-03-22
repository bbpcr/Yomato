
// Package downloader implements basic functions for downloading a torrent file
package downloader

import (
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/bbpcr/Yomato/bencode"
	"github.com/bbpcr/Yomato/local_server"
	"github.com/bbpcr/Yomato/peer"
	"github.com/bbpcr/Yomato/torrent_info"
	"github.com/bbpcr/Yomato/tracker"
)

type Downloader struct {
	Tracker     tracker.Tracker
	TorrentInfo torrent_info.TorrentInfo
	LocalServer *local_server.LocalServer
	PeerId      string
}

func (downloader Downloader) RequestPeersAndRequestHandshake(comm chan peer.PeerCommunication, bytesUploaded, bytesDownloaded, bytesLeft int64) (peersCount int, err error) {

	// Request the peers , from the tracker
	// The first paramater is how many bytes uploaded , the second downloaded , and the third remaining size
	data, err := downloader.Tracker.RequestPeers(bytesUploaded, bytesDownloaded, bytesLeft)

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
				newPeer.Handshake(comm)
			}
		}
	}
	return len(peers.Values), nil
}

func (downloader Downloader) SendInterestedAndUnchokedToPeers(peersList []peer.Peer) {

	// We send an unchoke message to the peers
	comm := make(chan peer.PeerCommunication)

	for index, _ := range peersList {
		peersList[index].SendUnchoke(comm)
	}

	// We wait for all of them to finish sending
	for numTotal := 0; numTotal < len(peersList); numTotal++ {
		select {
		case msg, _ := <-comm:
			status := msg.Message
			peer := msg.Peer
			fmt.Println("Sent unchoked to ", peer.RemotePeerId, " and received bytes : ", msg.BytesReceived, " with status : ", status)
		}
	}

	// We send an interested message to the peers
	for index, _ := range peersList {
		peersList[index].SendInterested(comm)
	}

	// We wait for all of them to finish sending
	for numTotal := 0; numTotal < len(peersList); numTotal++ {
		select {
		case msg, _ := <-comm:
			status := msg.Message
			peer := msg.Peer
			fmt.Println("Sent interested to ", peer.RemotePeerId, " and received ", status)
		}
	}
	return
}

// GetFileContents iterates through a list of peers and returns a new list containing the information of
// downloaded content for each of the peer.
func (downloader Downloader) GetFileContents(peersList []peer.Peer) []peer.Peer {
	comm := make(chan peer.PeerCommunication)

    // Next 2 lines wouldn't be better if integrated in a go func() ?
	for index, _ := range peersList {
		peersList[index].ReadExistingPieces(comm)
	}

	// We wait for all of them to finish sending
	var newPeersList []peer.Peer

	for numTotal := 0; numTotal < len(peersList); numTotal++ {
		select {
		case msg, _ := <-comm:
			peer := msg.Peer
			newPeersList = append(newPeersList, *peer)
			fmt.Println("Peer with ID : ", msg.Peer.RemotePeerId, " HAS : ", peer.BitfieldInfo, " with status : ", msg.Message)
		}
	}

	return newPeersList
}

// StartDownloading downloads the motherfucker
func (downloader Downloader) StartDownloading() {

	comm := make(chan peer.PeerCommunication)
	peersCount, err := downloader.RequestPeersAndRequestHandshake(comm, 0, 0, 10000)

	if err != nil {
		panic(err)
	}

	// At this point , we have loop where we wait for all the peers to complete their handshake or not.
	// We wait for the message to come from another goroutine , and we parse it.
	var goodPeers []peer.Peer
	numOk := 0

	for numTotal := 0; numTotal < peersCount; numTotal++ {
		select {
		case msg, _ := <-comm:
			peer := msg.Peer
			status := msg.Message
			if status == "Handshake OK" {
				numOk++
				goodPeers = append(goodPeers, *peer)
			} else if strings.Contains(status, "Error at handshake") {

			}
			fmt.Printf("\n-------------------------\n%sStatus Message : %s\nPeers OK : %d/%d\n-------------------------\n", peer.GetInfo(), status, numOk, numTotal)
		}
	}

	// We wait for peers to tell us , what pieces they have.
	// This is mandatory , since peers always send this first.
	goodPeers = downloader.GetFileContents(goodPeers)

	// We send an interested message to all peers
	downloader.SendInterestedAndUnchokedToPeers(goodPeers)

	// We request a piece just to check if it receives
	for index, _ := range goodPeers {
		goodPeers[index].RequestPiece(comm, 0, 0, 1<<14)
	}

	for numTotal := 0; numTotal < len(goodPeers); numTotal++ {
		select {
		case msg, _ := <-comm:
			peer := msg.Peer
			status := msg.Message
			if status == "Request OK" {
				fmt.Println("Requested from ", peer.RemotePeerId, " and received : ", msg.BytesReceived)
			} else {
				fmt.Println("Requested from ", peer.RemotePeerId, " and received : ", status)
			}
		}
	}

	// We disconnect the peers so they dont remain connected after use
	for index, _ := range goodPeers {
		defer goodPeers[index].Disconnect()
	}

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

	peerId := createPeerId()
	downloader := &Downloader{
		TorrentInfo: *torrentInfo,
		PeerId:      peerId,
	}
	downloader.LocalServer = local_server.New(peerId)
	tracker := tracker.New(torrentInfo, downloader.LocalServer.Port, peerId)
	downloader.Tracker = tracker
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
