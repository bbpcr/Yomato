package downloader

import (
	"crypto/rand"
	"fmt"
	"io/ioutil"

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

func (dowloader Downloader) Start() {
	data, err := dowloader.Tracker.Start()
	if err != nil {
		panic(err)
	}
	comm := make(chan peer.PeerCommunication)
	responseDictionary, isDictionary := data.(*bencode.Dictionary)

	if !isDictionary {
		return
	}

	peers, isList := responseDictionary.Values[bencode.String{"peers"}].(bencode.List)

	if !isList {
		return
	}

	for _, peerEntry := range peers.Values {
		peerData := peerEntry.(bencode.Dictionary).Values
		ip := peerData[bencode.String{"ip"}].(bencode.String).Value
		port := peerData[bencode.String{"port"}].(bencode.Number).Value
		newPeer := peer.New(&dowloader.TorrentInfo, dowloader.PeerId, ip, int(port))
		newPeer.Handshake(comm)
	}

	numOk := 0
	totalNum := 0
	for {
		select {
		case msg, _ := <-comm:
			peer := msg.Peer
			status := msg.Message
			totalNum++
			if status == "OK" {
				numOk++
			}
			fmt.Printf("\n-------------------------\n%sStatus Message : %s\nPeers OK : %d/%d\n-------------------------\n", peer.GetInfo(), status, numOk, totalNum)
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
