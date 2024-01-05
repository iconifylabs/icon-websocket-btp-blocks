package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/icon-project/goloop/client"
	"github.com/icon-project/goloop/common"
	"github.com/icon-project/goloop/common/codec"
	"github.com/icon-project/goloop/module"
	"github.com/icon-project/goloop/server"
)

const (
	MAINNET         = "https://ctz.solidwallet.io/api/v3/"
	MAX_CONCURRENCY = 100
	DEBUG_PORT      = 6060
	RPCCallRetry    = 5
)

func newHexInt(i int64) common.HexInt64 {
	return common.HexInt64{Value: i}
}

type btpBlockHeaderFormat struct {
	MainHeight             int64
	Round                  int32
	NextProofContextHash   []byte
	NetworkSectionToRoot   []module.MerkleNode
	NetworkID              int64
	UpdateNumber           int64
	PrevNetworkSectionHash []byte
	MessageCount           int64
	MessagesRoot           []byte
	NextProofContext       []byte
}

func base64rlp(encoded string, v interface{}) ([]byte, error) {
	if encoded == "" {
		return nil, fmt.Errorf("encoded string is empty ")
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}

	return codec.RLP.UnmarshalFromBytes(decoded, v)
}

func receiveLoop(ctx context.Context, client *client.ClientV3, startHeight int64) error {

	blockReq := server.BTPRequest{
		Height:    newHexInt(startHeight),
		NetworkId: common.HexInt64{Value: 1},
	}

	errCh := make(chan error)                                  // error channel
	reconnectCh := make(chan struct{}, 1)                      // reconnect channel
	btpBlockNotifCh := make(chan *server.BTPNotification, 100) // block notification channel

	reconnect := func() {
		fmt.Println("reconnecting------")
		select {
		case reconnectCh <- struct{}{}:
		default:
		}
		for len(btpBlockNotifCh) > 0 {
			select {
			case <-btpBlockNotifCh: // clear block notification channel
			}
		}
	}

	next := int64(startHeight) // next block height to process

	// subscribe to monitor block
	ctxMonitorBlock, cancelMonitorBlock := context.WithCancel(ctx)
	reconnect()
	for {
		select {
		case <-ctx.Done():
			return nil

		case err := <-errCh:
			return err

		// reconnect channel
		case <-reconnectCh:
			cancelMonitorBlock()
			ctxMonitorBlock, cancelMonitorBlock = context.WithCancel(ctx)

			// start new monitor loop
			go func(ctx context.Context, cancel context.CancelFunc) {
				defer cancel()
				blockReq.Height = newHexInt(next)

				err := client.MonitorBtp(&blockReq, func(v *server.BTPNotification) {
					if !errors.Is(ctx.Err(), context.Canceled) {
						btpBlockNotifCh <- v
					}
				}, nil)

				if err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					time.Sleep(time.Second * 5)
					reconnect()
				}
			}(ctxMonitorBlock, cancelMonitorBlock)

		default:
			select {
			default:
			case bn := <-btpBlockNotifCh:
				var hdr btpBlockHeaderFormat
				_, err := base64rlp(bn.Header, &hdr)
				if err != nil {
					log.Fatal(err)
				}
				fmt.Println("btp block received: ", hdr.MainHeight)
				if hdr.NextProofContext != nil {
					fmt.Println("Use this height to update client. Proof Context has changed----", hdr.MainHeight)
				}
			}
		}
	}

}

// Give the height to start with
// The websocket will restart after it's caught up
// Example: Query the client state on neutron, and give this height to start here
// Collect heights from this output

// btp block received:  75861524
// btp block received:  75862025
// Use this height to update client. Proof Context has changed---- 75862025
// btp block received:  75897298
// btp block received:  75904483
// Use this height to update client. Proof Context has changed---- 75904483
// btp block received:  75905145
// Use this height to update client. Proof Context has changed---- 75905145

// Update the client on neutron as
// rly tx update-client icon neutron icon-neutron 75862025
// rly tx update-client icon neutron icon-neutron 75904483
// rly tx update-client icon neutron icon-neutron 75905145
// OR
// rly tx update-client icon neutron icon-neutron 75862025,75904483,75905145

func main() {
	ctx := context.Background()
	cl := client.NewClientV3(MAINNET)

	startHeight := int64(75861363)
	receiveLoop(ctx, cl, startHeight)

}
