package piece_manager

import (
	"errors"
	"sync"

	"github.com/bbpcr/Yomato/file_writer"
	"github.com/bbpcr/Yomato/peer"
	"github.com/bbpcr/Yomato/torrent_info"
)

const (
	BLOCK_LENGTH = 1 << 14
)

type PieceManager struct {
	blockBytes       map[int]int  //tells me how much i need to download from a block [block:bytes]
	blockOffset      map[int]int  //tells me the offset of the block in piece [block:pieceOffset]
	blockDownloading map[int]bool //tells me if a block is downloading [block:true/false]
	blockPiece       map[int]int  //tells me what piece the block belongs [block:piece]
	pieceBytes       map[int]int  //tells me how much i downloaded from a piece [piece:bytes]
	pieceNumBlocks   map[int]int  //tells me how many blocks a piece has until his position [piece:numBlocks]
	rwMutex          sync.RWMutex
	//These should be maps because, if a value doesnt exist then we dont download it.
}

func (manager PieceManager) GetBlockIndex(pieceIndex int, offsetIndex int) int {

	startPosition := manager.pieceNumBlocks[pieceIndex]
	howMany := offsetIndex / BLOCK_LENGTH
	startPosition += howMany
	return startPosition
}

func New(torrentInfo *torrent_info.TorrentInfo) PieceManager {

	manager := PieceManager{
		blockBytes:       make(map[int]int),
		blockOffset:      make(map[int]int),
		blockDownloading: make(map[int]bool),
		blockPiece:       make(map[int]int),
		pieceBytes:       make(map[int]int),
		pieceNumBlocks:   make(map[int]int),
	}

	blockIndex := 0

	for pieceIndex := 0; pieceIndex < int(torrentInfo.FileInformations.PieceCount); pieceIndex++ {

		manager.pieceNumBlocks[pieceIndex] = blockIndex
		pieceLength := torrentInfo.FileInformations.PieceLength
		if pieceIndex == int(torrentInfo.FileInformations.PieceCount)-1 {
			pieceLength = torrentInfo.FileInformations.TotalLength - torrentInfo.FileInformations.PieceLength*(torrentInfo.FileInformations.PieceCount-1)
		}
		numBlocks := pieceLength / BLOCK_LENGTH
		lastBlockSize := pieceLength % BLOCK_LENGTH
		offset := 0

		for blockPosition := 0; blockPosition < int(numBlocks); blockPosition++ {
			manager.blockBytes[blockIndex] = BLOCK_LENGTH
			manager.blockDownloading[blockIndex] = false
			manager.blockPiece[blockIndex] = pieceIndex
			manager.blockOffset[blockIndex] = offset
			blockIndex++
			offset += BLOCK_LENGTH
		}

		manager.pieceBytes[pieceIndex] = 0

		if lastBlockSize != 0 {
			manager.blockBytes[blockIndex] = int(lastBlockSize)
			manager.blockDownloading[blockIndex] = false
			manager.blockPiece[blockIndex] = pieceIndex
			manager.blockOffset[blockIndex] = offset
			blockIndex++
		}
	}
	return manager
}

// Returns the ID of the next piece to download.
// This can use multiple strategies, e.g.
// Sequentially (NOT good, easy for development)
// or randomized (much better)
func (manager PieceManager) GetNextBlocksToDownload(for_peer *peer.Peer, maxBlocks int) []int {

	blocks := []int{}
	for block, count := 0, 0; block < len(manager.blockDownloading) && count < maxBlocks; block++ {
		_, exists := manager.blockBytes[block]
		if exists && !manager.blockDownloading[block] && for_peer.BitfieldInfo.At(manager.blockPiece[block]) && manager.blockBytes[block] > 0 {
			blocks = append(blocks, block)
			count++
		}
	}

	if len(blocks) < maxBlocks {
		for block, count := 0, len(blocks); block < len(manager.blockDownloading) && count < maxBlocks; block++ {
			_, exists := manager.blockBytes[block]
			if exists && for_peer.BitfieldInfo.At(manager.blockPiece[block]) && manager.blockBytes[block] > 0 {
				blocks = append(blocks, block)
				count++
			}
		}
	}

	return blocks
}

func (manager *PieceManager) UpdatePiece(pieceData file_writer.PieceData) error {

	manager.rwMutex.Lock()
	defer manager.rwMutex.Unlock()
	blockIndex := manager.GetBlockIndex(pieceData.PieceNumber, pieceData.Offset)
	pieceLength := len(pieceData.Piece)
	if manager.blockBytes[blockIndex] > 0 && pieceLength > 0 {
		manager.blockBytes[blockIndex] -= pieceLength
		manager.pieceBytes[pieceData.PieceNumber] += pieceLength
	} else {
		return errors.New("Piece wasn't updated due to invalid params or piece already updated")
	}
	return nil
}

func (manager *PieceManager) SetPieceDownloading(pieceData file_writer.PieceData, value bool) {
	manager.rwMutex.Lock()
	defer manager.rwMutex.Unlock()
	blockIndex := manager.GetBlockIndex(pieceData.PieceNumber, pieceData.Offset)
	manager.blockDownloading[blockIndex] = value
}

func (manager *PieceManager) SetBlockDownloading(blockIndex int, value bool) {
	manager.rwMutex.Lock()
	defer manager.rwMutex.Unlock()
	manager.blockDownloading[blockIndex] = value
}

func (manager PieceManager) MakeRequest(blockIndex int) (int, int, int) {

	pieceIndex := manager.blockPiece[blockIndex]
	pieceOffset := manager.blockOffset[blockIndex]
	pieceLength := manager.blockBytes[blockIndex]
	return pieceIndex, pieceOffset, pieceLength
}

func (manager PieceManager) IsPieceCompleted(pieceIndex int, torrentInfo *torrent_info.TorrentInfo) bool {
	if pieceIndex == int(torrentInfo.FileInformations.PieceCount-1) {
		if torrentInfo.FileInformations.PieceCount >= 2 {
			lastPieceLength := torrentInfo.FileInformations.TotalLength - torrentInfo.FileInformations.PieceLength*(torrentInfo.FileInformations.PieceCount-1)
			if int64(manager.pieceBytes[pieceIndex]) >= lastPieceLength {
				return true
			}
		}

	} else if int64(manager.pieceBytes[pieceIndex]) >= torrentInfo.FileInformations.PieceLength {
		return true
	}
	return false
}
