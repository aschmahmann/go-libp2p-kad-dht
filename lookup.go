package dht

import (
	"context"
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/routing"

	cid "github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log"
	pb "github.com/libp2p/go-libp2p-kad-dht/pb"
	kb "github.com/libp2p/go-libp2p-kbucket"
)

func tryFormatLoggableKey(k string) (string, error) {
	if len(k) == 0 {
		return "", fmt.Errorf("loggableKey is empty")
	}
	var proto, cstr string
	if k[0] == '/' {
		// it's a path (probably)
		protoEnd := strings.IndexByte(k[1:], '/')
		if protoEnd < 0 {
			return k, fmt.Errorf("loggableKey starts with '/' but is not a path: %x", k)
		}
		proto = k[1 : protoEnd+1]
		cstr = k[protoEnd+2:]
	} else {
		proto = "provider"
		cstr = k
	}

	c, err := cid.Cast([]byte(cstr))
	if err != nil {
		return "", fmt.Errorf("loggableKey could not cast key to a CID: %x %v", k, err)
	}
	return fmt.Sprintf("/%s/%s", proto, c.String()), nil
}

func loggableKey(k string) logging.LoggableMap {
	newKey, err := tryFormatLoggableKey(k)
	if err != nil {
		logger.Debug(err)
	} else {
		k = newKey
	}

	return logging.LoggableMap{
		"key": k,
	}
}

func (dht *IpfsDHT) GetClosestPeersSingle(ctx context.Context, from peer.ID, key string) ([]*peer.AddrInfo, error){
	pmes, err := dht.findPeerSingle(ctx, from, peer.ID(key))
	if err != nil {
		logger.Debugf("error getting closer peers: %s", err)
		return nil, err
	}
	peers := pb.PBPeersToPeerInfos(pmes.GetCloserPeers())
	return peers, nil
}

// Kademlia 'node lookup' operation. Returns a channel of the K closest peers
// to the given key
func (dht *IpfsDHT) GetClosestPeers(ctx context.Context, key string) (<-chan peer.ID, error) {
	e := logger.EventBegin(ctx, "getClosestPeers", loggableKey(key))
	tablepeers := dht.routingTable.NearestPeers(kb.ConvertKey(key), KValue)
	if len(tablepeers) == 0 {
		return nil, kb.ErrLookupFailure
	}

	out := make(chan peer.ID, KValue)

	query := dht.newClosestPeersQuery(ctx, key, nil)

	go func() {
		defer close(out)
		defer e.Done()
		// run it!
		res, err := query.Run(ctx, tablepeers)
		if err != nil {
			logger.Debugf("closestPeers query run error: %s", err)
		}

		for _, p := range res {
			out <- p
		}
	}()

	return out, nil
}

func (dht *IpfsDHT) newClosestPeersQuery(ctx context.Context, key string, finish finishFunc) *dhtQuery {
	// since the query doesnt actually pass our context down
	// we have to hack this here. whyrusleeping isnt a huge fan of goprocess
	parent := ctx
	return dht.newQuery(key, func(ctx context.Context, p peer.ID) ([]*peer.AddrInfo, error) {
		// For DHT query command
		routing.PublishQueryEvent(parent, &routing.QueryEvent{
			Type: routing.SendingQuery,
			ID:   p,
		})

		pmes, err := dht.findPeerSingle(ctx, p, peer.ID(key))
		if err != nil {
			logger.Debugf("error getting closer peers: %s", err)
			return nil, err
		}
		peers := pb.PBPeersToPeerInfos(pmes.GetCloserPeers())

		// For DHT query command
		routing.PublishQueryEvent(parent, &routing.QueryEvent{
			Type:      routing.PeerResponse,
			ID:        p,
			Responses: peers,
		})

		return peers, nil
	}, finish)
}
