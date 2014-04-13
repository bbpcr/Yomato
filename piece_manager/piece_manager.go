package piece_manager

import (

	"math/rand"
	
	"github.com/bbpcr/Yomato/peer"
	"github.com/bbpcr/Yomato/torrent_info"
)

const (
	BLOCK_LENGTH = 1 << 14
)

type PieceManager struct {
	BlockBytes map[int]int //tells me how much i need to download from a block [block:bytes]
	BlockOffset map[int]int //tells me the offset of the block in piece [block:pieceOffset]
	BlockDownloading map[int]bool //tells me if a block is downloading [block:true/false]
	BlockPiece map[int]int //tells me what piece the block belongs [block:piece]
	PieceBytes map[int]int //tells me how much i downloaded from a piece [piece:bytes]
	PieceNumBlocks map[int]int //tells me how many blocks a piece has [piece:numBlocks]
}

func (manager PieceManager) GetBlockIndex(pieceIndex int , offsetIndex int) int {
	startPosition := pieceIndex * manager.PieceNumBlocks[pieceIndex]
	howMany := offsetIndex / BLOCK_LENGTH
	if (offsetIndex % BLOCK_LENGTH != 0){
		howMany++
	}	
	startPosition += howMany
	return startPosition
}

func New(torrentInfo *torrent_info.TorrentInfo) PieceManager {

	rand.Seed(42)

	manager := PieceManager {
		BlockBytes : make(map[int]int),
		BlockOffset : make(map[int]int),
		BlockDownloading : make(map[int]bool),
		BlockPiece : make(map[int]int),
		PieceBytes : make(map[int]int),
		PieceNumBlocks : make(map[int]int),
	}
	
	blockIndex := 0
	
	for pieceIndex := 0; pieceIndex < int(torrentInfo.FileInformations.PieceCount); pieceIndex++ {
	
		pieceLength := torrentInfo.FileInformations.PieceLength
		if pieceIndex == int(torrentInfo.FileInformations.PieceCount) - 1 {
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
			blockIndex++;
			offset += BLOCK_LENGTH
		}
		
		manager.PieceBytes[pieceIndex] = 0
		manager.PieceNumBlocks[pieceIndex] = int(numBlocks)
		
		if (lastBlockSize != 0) {
			manager.BlockBytes[blockIndex] = int(lastBlockSize)
			manager.BlockDownloading[blockIndex] = false
			manager.BlockPiece[blockIndex] = pieceIndex
			manager.PieceNumBlocks[pieceIndex] ++
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
func (manager PieceManager) GetNext5BlocksToDownload(for_peer *peer.Peer) ([]int) {
	var blocks []int
	count := 0
	for block := 0 ; block < len(manager.BlockDownloading) && count < 5; block++ {
		_ , exists := manager.BlockBytes[block]
		if exists && !manager.BlockDownloading[block] && for_peer.BitfieldInfo.At(manager.BlockPiece[block]) && manager.BlockBytes[block] > 0 {
			blocks = append(blocks , block)
			count ++
		}
	}
	return blocks
}

// Returns the ID of the next piece to download.
// This can use multiple strategies, e.g.
// Sequentially (NOT good, easy for development)
// or randomized (much better)

func (manager PieceManager) Get5RandomBlocksToDownload(for_peer *peer.Peer) ([]int) {
	var blocks []int
	count := 0
	for count < 1 {
		block := int(rand.Int31n(int32(len(manager.BlockDownloading))))
		_ , exists := manager.BlockBytes[block]
		if exists && !manager.BlockDownloading[block] && for_peer.BitfieldInfo.At(manager.BlockPiece[block]) && manager.BlockBytes[block] > 0 {
			blocks = append(blocks , block)
			count ++
		}
	}
	return blocks
}