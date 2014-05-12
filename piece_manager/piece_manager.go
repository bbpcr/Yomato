package piece_manager

import (


	"github.com/bbpcr/Yomato/peer"
	"github.com/bbpcr/Yomato/torrent_info"
)

const (
	BLOCK_LENGTH = 1 << 14
)

type PieceManager struct {
	BlockBytes       map[int]int  //tells me how much i need to download from a block [block:bytes]
	BlockOffset      map[int]int  //tells me the offset of the block in piece [block:pieceOffset]
	BlockDownloading map[int]bool //tells me if a block is downloading [block:true/false]
	BlockPiece       map[int]int  //tells me what piece the block belongs [block:piece]
	PieceBytes       map[int]int  //tells me how much i downloaded from a piece [piece:bytes]
	pieceNumBlocks   map[int]int  //tells me how many blocks a piece has until his position [piece:numBlocks]
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
		BlockBytes:       make(map[int]int),
		BlockOffset:      make(map[int]int),
		BlockDownloading: make(map[int]bool),
		BlockPiece:       make(map[int]int),
		PieceBytes:       make(map[int]int),
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
			manager.BlockBytes[blockIndex] = BLOCK_LENGTH
			manager.BlockDownloading[blockIndex] = false
			manager.BlockPiece[blockIndex] = pieceIndex
			manager.BlockOffset[blockIndex] = offset
			blockIndex++
			offset += BLOCK_LENGTH
		}

		manager.PieceBytes[pieceIndex] = 0

		if lastBlockSize != 0 {
			manager.BlockBytes[blockIndex] = int(lastBlockSize)
			manager.BlockDownloading[blockIndex] = false
			manager.BlockPiece[blockIndex] = pieceIndex
			manager.BlockOffset[blockIndex] = offset
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
	var blocks []int = nil
	for block, count := 0, 0; block < len(manager.BlockDownloading) && count < maxBlocks; block++ {
		_, exists := manager.BlockBytes[block]
		if exists && !manager.BlockDownloading[block] && for_peer.BitfieldInfo.At(manager.BlockPiece[block]) && manager.BlockBytes[block] > 0 {
			blocks = append(blocks, block)
			count++
		}
	}
	return blocks
}

func (manager PieceManager) IsPieceCompleted(pieceIndex int, torrentInfo *torrent_info.TorrentInfo) bool {

	if pieceIndex == int(torrentInfo.FileInformations.PieceCount-1) {
		// If it's the last piece , we need to treat it better.
		// The last piece has lesser size
		if torrentInfo.FileInformations.PieceCount >= 2 {
			lastPieceLength := torrentInfo.FileInformations.TotalLength - torrentInfo.FileInformations.PieceLength*(torrentInfo.FileInformations.PieceCount-1)
			if int64(manager.PieceBytes[pieceIndex]) >= lastPieceLength {
				return true
			}
		}

	} else if int64(manager.PieceBytes[pieceIndex]) >= torrentInfo.FileInformations.PieceLength {
		return true
	}
	return false
}