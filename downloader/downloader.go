package downloader

import (
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"strings"
	//"time"

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

func (downloader Downloader) SendInterestedToPeers(peersList []peer.Peer) {

	// We send and interested message to the peers

	comm := make(chan peer.PeerCommunication)

	for index, _ := range peersList {
		peersList[index].SendInterested(comm)
	}

	// We wait for all of them to finish sending

	numTotal := 0
	for numTotal < len(peersList) {
		select {
		case msg, _ := <-comm:
			numTotal++
			status := msg.Message
			peer := msg.Peer
			fmt.Println("Sent interested to ", peer.RemotePeerId, " and received ", status)
		}
	}
	return
}

func (downloader Downloader) StartDownloading() {

	comm := make(chan peer.PeerCommunication)
	peersCount, err := downloader.RequestPeersAndRequestHandshake(comm, 0, 0, 10000)

	if err != nil {
		panic(err)
	}

	numTotal := 0
	numOk := 0

	// At this point , we have loop where we wait for all the peers to complete their handshake or not.
	// We wait for the message to come from another goroutine , and we parse it.

	var goodPeers []peer.Peer

	for numTotal < peersCount {
		select {
		case msg, _ := <-comm:
			peer := msg.Peer
			status := msg.Message
			numTotal++
			if status == "Handshake OK" {
				numOk++
				goodPeers = append(goodPeers, *peer)
			} else if strings.Contains(status, "Error at handshake") {

			}
			fmt.Printf("\n-------------------------\n%sStatus Message : %s\nPeers OK : %d/%d\n-------------------------\n", peer.GetInfo(), status, numOk, numTotal)
		}
	}

	// We send an interested message to all peers

	downloader.SendInterestedToPeers(goodPeers)

	// We request a piece just to check if it receives

	for index, _ := range goodPeers {
		goodPeers[index].RequestPiece(comm, 0, 0, 1<<15)
	}

	numTotal = 0
	for numTotal < len(goodPeers) {
		select {
		case msg, _ := <-comm:
			peer := msg.Peer
			status := msg.Message
			numTotal++
			if status == "Request OK" {
				fmt.Println("Requested from ", peer.RemotePeerId, " and received : ", msg.BytesReceived)
			} else {
				fmt.Println("Requested from ", peer.RemotePeerId, " and received : ", status)
			}
		}
	}
	return
}

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
