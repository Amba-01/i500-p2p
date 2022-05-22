package p2p

import (
	"context"
	"encoding/json"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	log "github.com/sirupsen/logrus"
)

const topicName = "/bitcoin-simulation/1.0.6"

type Consortium struct {
	ctx          context.Context
	selfId       peer.ID
	topic        *pubsub.Topic
	subscription *pubsub.Subscription
	ps           *pubsub.PubSub
	Host         *Node
	Messages     chan *Message
	Headers      []string
}

type Message struct {
	SenderID peer.ID `json:"sender_id"`
	Message  string  `json:"message"`
}

func JoinConsortium(ctx context.Context, p2pHost *Node) (*Consortium, error) {
	topic, err := p2pHost.PubSub.Join(topicName)

	if err != nil {
		return nil, err
	}

	sub, err := topic.Subscribe()
	if err != nil {
		return nil, err
	}

	consortium := &Consortium{
		ctx:          ctx,
		subscription: sub,
		selfId:       p2pHost.Host.ID(),
		topic:        topic,
		ps:           p2pHost.PubSub,
		Host:         p2pHost,
		Messages:     make(chan *Message, 1024),
		Headers:      make([]string, 0),
	}

	go consortium.SubscribeLoop()

	return consortium, nil
}

func (ctm *Consortium) SubscribeLoop() {
	for {
		inboundMsg, err := ctm.subscription.Next(ctm.ctx)

		if err != nil {
			close(ctm.Messages)
			return
		}

		if inboundMsg.ReceivedFrom == ctm.selfId {
			continue
		}

		msg := &Message{}
		err = json.Unmarshal(inboundMsg.Data, msg)
		if err != nil {
			continue
		}

		log.Infoln("Received message:", msg)

		ctm.Messages <- msg
	}
}

func (ctm *Consortium) Publish(msg *Message) error {

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	err = ctm.topic.Publish(ctm.ctx, msgBytes)
	if err != nil {
		return err
	}
	log.Printf("-Published Message(%d chars): %s\n", len(msgBytes), string(msgBytes))
	return nil
}

func PeriodicBroadcast(ctm *Consortium) {
	for {
		time.Sleep(time.Second * 15)
		peers := ctm.ps.ListPeers(topicName)
		log.Printf("- Found %d other peers in the network: %s\n", len(peers), peers)

		message := Message{
			Message:  "i am a message",
			SenderID: ctm.Host.Host.ID(),
		}

		if err := ctm.Publish(&message); err != nil {
			log.Println("- Error publishing IHAVE message on the network:", err)
		}
	}

}
