package downloader

import (
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

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
			if ipIsString && portIsNumber {

				// We try to make a handshake with the peer.
				// Results are sent on the channel comm.

				newPeer := peer.New(&downloader.TorrentInfo, downloader.PeerId, ip.Value, int(port.Value))
				newPeer.Handshake(comm)

				// We sleep a bit , because we dont want to overload the CPU
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
	return len(peers.Values), nil
}

func (downloader Downloader) StartDownloading() {

	comm := make(chan peer.PeerCommunication)
	_, err := downloader.RequestPeersAndRequestHandshake(comm, 0, 0, 10000)

	if err != nil {
		panic(err)
	}

	numTotal := 0
	numOk := 0

	// At this point , we have an infinite loop.
	// We wait for the message to come from another goroutine , and we parse it.

	for {
		select {
		case msg, _ := <-comm:
			peer := msg.Peer
			status := msg.Message
			numTotal++
			if status == "Handshake OK" {
				numOk++
				fmt.Printf("\n-------------------------\n%sStatus Message : %s\nPeers OK : %d/%d\n-------------------------\n", peer.GetInfo(), status, numOk, numTotal)
			} else if strings.Contains(status, "Error at handshake") {

			}
		}
	}
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
