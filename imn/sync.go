// sync.go

package imn

import (
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum/log"
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core/types"
	imnapi "github.com/ethereum/go-ethereum/imn/api"
	imnminer "github.com/ethereum/go-ethereum/imn/miner"
	"github.com/ethereum/go-ethereum/params"
)

// cached governance data to derive miner's enode
type coinbaseEnodeEntry struct {
	modifiedBlock  *big.Int
	nodes          []*imnNode
	coinbase2enode map[string][]byte // string(common.Address[:]) => []byte
	enode2index    map[string]int    // string([]byte) => int
}

const (
	imnWorkKey    = "work"
	imnLockKey    = "lock"
	MiningLockTTL = 10 // seconds
)

var (
	syncLock = &sync.Mutex{}

	// the latest block info: *core.types.Header
	latestBlock atomic.Value

	// the latest etcd leader ID: uint64
	latestEtcdLeader atomic.Value

	// the latest mining token info: []byte, json'ed ImnEtcdLock
	latestMiningToken atomic.Value

	// the latest work info: []byte, json'ed imnWork
	latestImnWork atomic.Value

	// governance data at modifiedBlock height
	// cached coinbase -> enode & enode existence check at given modifiedBlock
	// sync.Map[int]*cointbaseEnodeEntry
	coinbaseEnodeCache = &sync.Map{}

	// lru cache: block height => enode
	height2enode = lru.NewLruCache(10000, true)

	// cached mining peer status
	// sync.Map[string]*imnapi.IMNMinerStatus
	miningPeers = &sync.Map{}

	// mining token
	miningToken atomic.Value
)

func (ma *imnAdmin) handleNewBlocks() {
	ch := make(chan *types.Header, 128)
	sub, err := ma.cli.SubscribeNewHead(context.Background(), ch)
	if err != nil {
		panic(fmt.Sprintf("cannot subscribe to new block head: %v", err))
	}
	defer sub.Unsubscribe()

	for {
		typeHeader := <-ch
		if typeHeader == nil {
			log.Debug("typeHeader is nil")
		}
		if header, err := admin.cli.HeaderByNumber(context.Background(), nil); err == nil {
			latestBlock.Store(header)
			refreshCoinbaseEnodeCache(header.Number)
		}
		admin.update()
	}
}

func handleMinerStatusUpdate() {
	ch := make(chan *imnapi.IMNMinerStatus, 128)
	sub := imnapi.SubscribeToMinerStatus(ch)
	defer func() {
		sub.Unsubscribe()
		close(ch)
	}()
	for {
		status := <-ch
		miningPeers.Store(status.NodeName, status.Clone())
	}
}

func isBootNodeBeforeGenesis() bool {
	if params.ConsensusMethod == params.ConsensusPoW {
		return true
	} else if params.ConsensusMethod == params.ConsensusETCD {
		return false
	} else if params.ConsensusMethod == params.ConsensusPoA {
		if admin == nil {
			return false
		} else if admin.self == nil || len(admin.nodes) <= 0 {
			if admin.nodeInfo != nil && admin.nodeInfo.ID == admin.bootNodeId {
				return true
			} else {
				return false
			}
		}
	}
	return false
}

func loadMiningToken() *ImnEtcdLock {
	var (
		lck *ImnEtcdLock
		ok  bool
	)
	if lckData := miningToken.Load(); lckData != nil {
		if lck, ok = lckData.(*ImnEtcdLock); !ok {
			return nil
		}
	}
	return lck
}

func acquireMiningToken(height *big.Int, parentHash common.Hash) (bool, error) {
	if isBootNodeBeforeGenesis() {
		return true, nil
	}
	if admin == nil || !admin.etcdIsRunning() {
		return false, ErrNotRunning
	}
	ctx, cancel := context.WithTimeout(context.Background(),
		admin.etcd.Server.Cfg.ReqTimeout())
	defer cancel()
	lck, err := admin.acquireLockSync(ctx, height, parentHash, MiningLockTTL)
	if err != nil {
		return false, err
	}
	miningToken.Store(lck)
	return true, nil
}

// log the latest block & release the mining token
func releaseMiningToken(height *big.Int, hash, parentHash common.Hash) error {
	if isBootNodeBeforeGenesis() {
		return nil
	}
	lck := loadMiningToken()
	if lck == nil || lck.ttl() < 0 {
		return imnminer.ErrNotInitialized
	}
	ctx, cancel := context.WithTimeout(context.Background(),
		admin.etcd.Server.Cfg.ReqTimeout())
	defer cancel()
	err := lck.releaseLockSync(ctx, height, hash, parentHash)

	// invalidate the saved token
	miningToken.Store(&ImnEtcdLock{})
	return err
}

func hasMiningToken() bool {
	if isBootNodeBeforeGenesis() {
		return true
	}
	lck := loadMiningToken()
	if lck == nil || lck.ttl() < 0 {
		return false
	}
	return true
}

// EOF
