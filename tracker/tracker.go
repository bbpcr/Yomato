package tracker

import (
	"errors"
	"fmt"
	"github.com/bbpcr/Yomato/bencode"
	"github.com/bbpcr/Yomato/torrent_info"
	"io/ioutil"
	"net/http"
	"net/url"
)

type Tracker struct {
	TorrentInfo *torrent_info.TorrentInfo
	PeerId      string
	LocalServer *http.Server
	Port        int
}

func (tracker Tracker) RequestPeers(bytesUploaded, bytesDownloaded, bytesLeft int64) (bencode.Bencoder, error) {
	peerId := tracker.PeerId

	// We create the URL like this :
	// announcer?peer_id= & info_hash= & port= & uploaded= & downloaded= & left= & event=
	// The uploaded , downloaded and left should always be , but are not necesary

	qs := url.Values{}
	qs.Add("peer_id", peerId)
	qs.Add("info_hash", string(tracker.TorrentInfo.InfoHash))
	qs.Add("port", fmt.Sprintf("%d", tracker.Port))
	qs.Add("uploaded", fmt.Sprintf("%d", bytesUploaded))
	qs.Add("downloaded", fmt.Sprintf("%d", bytesDownloaded))
	qs.Add("left", fmt.Sprintf("%d", bytesLeft))
	qs.Add("event", "started")

	res, err := http.Get(tracker.TorrentInfo.AnnounceUrl + "?" + qs.Encode())
	if err != nil {
		return bencode.Dictionary{}, err
	}

	body, err := ioutil.ReadAll(res.Body)

	if err != nil {
		return bencode.Dictionary{}, err
	}
	if res.StatusCode == 200 {
		data, _, err := bencode.Parse(body)
		if err != nil {
			return bencode.Dictionary{}, err
		}

		newData, err := GetPeers(data)

		if err != nil {
			return bencode.Dictionary{}, err
		}

		return newData, nil
	}
	return bencode.Dictionary{}, errors.New(fmt.Sprintf("Expected 200 OK from tracker; got %s", res.Status))
}

func New(info *torrent_info.TorrentInfo, port int, peerId string) Tracker {
	tracker := Tracker{
		TorrentInfo: info,
		PeerId:      peerId,
		Port:        port,
	}
	return tracker
}
