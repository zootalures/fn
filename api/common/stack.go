package common

import (
	"context"
	"sync"
)

type Stack interface {
	Push() chan<- interface{}
	Pop() <-chan interface{}
}

type stack struct {
	cond *sync.Cond
	list []item
}

type item struct {
	ch   <-chan interface{}
	done <-chan struct{}
}

func newStack() *stack {
	return &stack{
		cond: sync.NewCond(new(sync.Mutex)),
	}
}

func (s *stack) Push(ctx context.Context) chan<- interface{} {
	ch := make(chan interface{})
	s.cond.L.Lock()
	s.list = append(s.list, item{ch: ch, done: ctx.Done()})
	s.cond.L.Unlock()
	s.cond.Broadcast()
	return ch
}

func (s *stack) Pop(ctx context.Context) <-chan interface{} {
	ch := make(chan interface{})

	go s.pop(ctx, ch)

	return ch
}

// TODO play wit dis?
//func (s *stack) popCh(ctx context.Context) <-chan <-chan interface{} {
//ch := make(chan <-chan interface{})

//go s.pop2(ctx, ch)

//return ch
//}

func (s *stack) pop(ctx context.Context, ch chan<- interface{}) {
	s.cond.L.Lock()
	defer s.cond.L.Unlock()

	for {
		for len(s.list) == 0 && ctx.Err() == nil {
			s.cond.Wait()
		}

		// TODO clean up should probably be push driven, if poppers dissipate there are still pushers

		// walk from head to tail, remove any goners
		for i := 0; i < len(s.list); i++ {
			select {
			case <-s.list[i].done:
				s.list = append(s.list[:i], s.list[i+1:]...)
				i--
			default:
				break // since this is LIFO this shouldn't be sparse, but we'll double check below
			}
		}

		// after clean up, if ctx is over, just bail
		if ctx.Err() != nil {
			return
		}

		// now walk from tail to head until somebody is there (should be tail, if anyone)
		for i := len(s.list) - 1; i > -1; i-- {
			in := s.list[i]
			s.list = s.list[:i]

			// NOTE we can't atomically receive / send (ch <- <-in.ch), deadlocks if sender on in.ch is gone
			select {
			default: // loop again
			case <-ctx.Done(): // XXX (reed): could do this before checking in.ch, which may beat this (it's random)
				return
			case it := <-in.ch:
				select {
				default:
					// XXX (reed): our use case needs to call it.Done() here if popper is gone but we recvd from a pusher.
					// see possible ideas around popCh?
				case ch <- it:
				}
				return
			}
		}

		// cond is still locked here
	}
}
