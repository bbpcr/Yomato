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

func (d Downloader) Start() {
	data, err := d.Tracker.Start()
	if err != nil {
		panic(err)
	}

	comm := make(chan peer.PeerCommunication)
	peers := data.(*bencode.Dictionary).Values[bencode.String{"peers"}].(bencode.List).Values
	for _, peerEntry := range peers {
		peerData := peerEntry.(bencode.Dictionary).Values
		ip := peerData[bencode.String{"ip"}].(bencode.String).Value
		port := peerData[bencode.String{"port"}].(bencode.Number).Value
		p := peer.New(&d.TorrentInfo, d.PeerId, ip, int(port))
		p.Handshake(comm)
	}

	numOk := 0
	totalNum := 0
	for {
		select {
		case msg, _ := <-comm:
			p := msg.Peer
			status := msg.Message
			totalNum++
			if status == "OK" {
				numOk++
			}
			fmt.Printf("\n-------------------------\nRemote IP: %s\nStatus: %s\nPeer ID: %s\nPeer ID length: %d\nPeers Ok: %d/%d\n-------------------------\n", p.IP, status, p.RemotePeerId, len(p.RemotePeerId), numOk, totalNum)
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
