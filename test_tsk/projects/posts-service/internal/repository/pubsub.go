package repository

import (
	"sync"

	"posts-service/internal/model"
)

type PubSub struct {
	mu          sync.RWMutex
	subscribers map[string][]chan *model.Comment
}

func NewPubSub() *PubSub {
	return &PubSub{
		subscribers: make(map[string][]chan *model.Comment),
	}
}

func (ps *PubSub) Subscribe(postID string) chan *model.Comment {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ch := make(chan *model.Comment, 1)
	ps.subscribers[postID] = append(ps.subscribers[postID], ch)
	return ch
}

func (ps *PubSub) Unsubscribe(postID string, ch chan *model.Comment) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	subs := ps.subscribers[postID]
	for i, sub := range subs {
		if sub == ch {
			ps.subscribers[postID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
}

func (ps *PubSub) Publish(postID string, comment *model.Comment) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	for _, ch := range ps.subscribers[postID] {
		select {
		case ch <- comment:
		default:
		}
	}
}
