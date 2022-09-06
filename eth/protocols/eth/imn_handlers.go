package eth

import (
	"fmt"

	imnapi "github.com/ethereum/go-ethereum/imn/api"
	imnminer "github.com/ethereum/go-ethereum/imn/miner"
)

func handleGetPendingTxs(backend Backend, msg Decoder, peer *Peer) error {
	// not supported, just ignore it.
	return nil
}

func handleGetStatusEx(backend Backend, msg Decoder, peer *Peer) error {
	if !imnminer.AmPartner() || !imnminer.IsPartner(peer.ID()) {
		return nil
	}

	go func() {
		statusEx := imnapi.GetMinerStatus()
		statusEx.LatestBlockTd = backend.Chain().GetTd(statusEx.LatestBlockHash,
			statusEx.LatestBlockHeight.Uint64())
		if err := peer.SendStatusEx(statusEx); err != nil {
			// ignore the error
		}
	}()

	return nil
}

func handleStatusEx(backend Backend, msg Decoder, peer *Peer) error {
	if !imnminer.AmPartner() || !imnminer.IsPartner(peer.ID()) {
		return nil
	}
	var status imnapi.IMNMinerStatus
	if err := msg.Decode(&status); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}

	go func() {
		if _, td := peer.Head(); status.LatestBlockTd.Cmp(td) > 0 {
			peer.SetHead(status.LatestBlockHash, status.LatestBlockTd)
		}
		imnapi.GotStatusEx(&status)
	}()

	return nil
}

func handleEtcdAddMember(backend Backend, msg Decoder, peer *Peer) error {
	if !imnminer.AmPartner() || !imnminer.IsPartner(peer.ID()) {
		return nil
	}

	go func() {
		cluster, _ := imnapi.EtcdAddMember(peer.ID())
		if err := peer.SendEtcdCluster(cluster); err != nil {
			// ignore the error
		}
	}()

	return nil
}

func handleEtcdCluster(backend Backend, msg Decoder, peer *Peer) error {
	if !imnminer.AmPartner() || !imnminer.IsPartner(peer.ID()) {
		return nil
	}
	var cluster string
	if err := msg.Decode(&cluster); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}

	go imnapi.GotEtcdCluster(cluster)

	return nil
}
