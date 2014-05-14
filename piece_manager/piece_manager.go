package piece_manager

import (
	"github.com/bbpcr/Yomato/bitfield"
	"github.com/bbpcr/Yomato/database"
	"github.com/bbpcr/Yomato/torrent_info"
	"math/rand"
	"sync"
)

const (
	BLOCK_LENGTH = 1 << 14
)

type PieceManager struct {
	blockBytes       map[int]int          // tells me how much I need to download from a block [block:bytes]
	blockOffset      map[int]int          // tells me the offset of the block in piece [block:pieceOffset]
	blockDownloading map[int]bool         // tells me if a block is downloading [block:true/false]
	blockPiece       map[int]int          // tells me what piece the block belongs [block:piece]
	pieceNumBlocks   map[int]int          // tells me how many blocks a piece has until his position [piece:numBlocks]
	pieceBlocks      map[int]map[int]bool // tells me the blocks left to download for each piece [piece:set[blocks]]
	pieceOrder       []int                // the order to download the pieces, it gets randomized everytime you restart the app
	pieceCount       int                  // the number of pieces
	blockCount       int                  // the number of blocks
	bytesDownloaded  int64                // the number of downloaded Bytes
	mutex            sync.Mutex           // a mutex to handle concurrency
	model            *database.Model      // the database connection
}

func New(torrentInfo *torrent_info.TorrentInfo, model *database.Model) *PieceManager {

	manager := &PieceManager{
		blockBytes:       make(map[int]int),
		blockOffset:      make(map[int]int),
		blockDownloading: make(map[int]bool),
		blockPiece:       make(map[int]int),
		pieceNumBlocks:   make(map[int]int),
		pieceBlocks:      make(map[int]map[int]bool),
		model:            model,
	}

	blockIndex := 0

	for pieceIndex := 0; pieceIndex < int(torrentInfo.FileInformations.PieceCount); pieceIndex++ {
		manager.pieceBlocks[pieceIndex] = make(map[int]bool)

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
			manager.pieceBlocks[pieceIndex][blockIndex] = true
			blockIndex++
			offset += BLOCK_LENGTH
		}

		if lastBlockSize != 0 {
			manager.blockBytes[blockIndex] = int(lastBlockSize)
			manager.blockDownloading[blockIndex] = false
			manager.blockPiece[blockIndex] = pieceIndex
			manager.blockOffset[blockIndex] = offset
			manager.pieceBlocks[pieceIndex][blockIndex] = true
			blockIndex++
		}
	}

	// remove blocks that have already been downloaded
	blocksDownloaded, err := model.BlocksDownloaded()
	if err != nil {
		panic(err)
	}

	for _, blockIndex := range blocksDownloaded {
		delete(manager.pieceBlocks[manager.blockPiece[blockIndex]], blockIndex)
		manager.bytesDownloaded += int64(manager.blockBytes[blockIndex])
		manager.blockBytes[blockIndex] = 0
	}

	manager.pieceCount = int(torrentInfo.FileInformations.PieceCount)
	manager.blockCount = blockIndex
	manager.pieceOrder = rand.Perm(manager.pieceCount)
	return manager
}

// Returns the next blocks to download
// Blocks are usually from the same piece (or multiple ones when they are the very few of that piece) but otherwise random
// The piece order is also random
func (manager *PieceManager) GetNextBlocksToDownload(possiblePieces *bitfield.Bitfield, maxBlocks int) []int {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	count := 0
	var blocks []int = nil
	for _, piece := range manager.pieceOrder {
		for block := range manager.pieceBlocks[piece] {
			if !manager.blockDownloading[block] && possiblePieces.At(piece) {
				blocks = append(blocks, block)
				count++
			}

			if count == maxBlocks {
				break
			}
		}

		if count == maxBlocks {
			break
		}
	}

	// last pieces; go all-out and ask for the same block to as many peers as possible
	if len(blocks) < maxBlocks {
		for _, piece := range manager.pieceOrder {
			for block := range manager.pieceBlocks[piece] {
				if possiblePieces.At(piece) {
					blocks = append(blocks, block)
					count++
				}
			}
		}
	}

	return blocks
}

func (manager *PieceManager) GetBlockIndex(pieceIndex int, offsetIndex int) int {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	startPosition := manager.pieceNumBlocks[pieceIndex]
	howMany := offsetIndex / BLOCK_LENGTH
	startPosition += howMany
	if startPosition < 0 || startPosition >= manager.blockCount || manager.blockOffset[startPosition] != offsetIndex {
		return -1
	}

	return startPosition
}

func (manager *PieceManager) IsPieceCompleted(pieceIndex int) bool {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	return len(manager.pieceBlocks[pieceIndex]) == 0
}

func (manager *PieceManager) MarkBlockDownloaded(blockIndex int) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	manager.bytesDownloaded += int64(manager.blockBytes[blockIndex])
	manager.blockBytes[blockIndex] = 0
	delete(manager.pieceBlocks[manager.blockPiece[blockIndex]], blockIndex)
}

func (manager *PieceManager) UnmarkPieceDownloaded(pieceIndex int, pieceSize int) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	startBlockIndex := manager.pieceNumBlocks[pieceIndex]
	numBlocks := int(pieceSize / BLOCK_LENGTH)
	manager.bytesDownloaded -= int64(pieceSize)
	for pieceBlock := 0; pieceBlock < numBlocks; pieceBlock++ {
		manager.blockBytes[startBlockIndex+pieceBlock] = BLOCK_LENGTH
		manager.pieceBlocks[pieceIndex][startBlockIndex+pieceBlock] = true
		manager.blockDownloading[startBlockIndex+pieceBlock] = false
	}

	lastBlockSize := pieceSize % BLOCK_LENGTH
	if lastBlockSize > 0 {
		manager.blockBytes[startBlockIndex+numBlocks] = lastBlockSize
		manager.pieceBlocks[pieceIndex][startBlockIndex+numBlocks] = true
		manager.blockDownloading[startBlockIndex+numBlocks] = false
	}
}

func (manager *PieceManager) BlockSizeCoresponds(blockIndex int, blockSize int) bool {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	if manager.blockBytes[blockIndex] == 0 || manager.blockBytes[blockIndex] != blockSize {
		return false
	}
	return true
}

func (manager *PieceManager) SetBlockDownloading(blockIndex int, downloading bool) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	manager.blockDownloading[blockIndex] = downloading
}

func (manager *PieceManager) RequestBlockInformation(blockIndex int) []int {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	return []int{manager.blockPiece[blockIndex], manager.blockOffset[blockIndex], manager.blockBytes[blockIndex]}
}

func (manager *PieceManager) BytesDownloaded() int64 {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	return manager.bytesDownloaded
}

func (manager *PieceManager) MakeRequest(blockIndex int) (pieceIndex int, pieceOffset int, pieceLength int) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	pieceIndex = manager.blockPiece[blockIndex]
	pieceOffset = manager.blockOffset[blockIndex]
	pieceLength = manager.blockBytes[blockIndex]
	return
}
